package ghpackage

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/google/go-github/v53/github"
	"golang.org/x/sync/errgroup"
)

type RetentionManager struct {
	OrganizationName           string
	PackageType                string
	PackageNames               []string
	Age                        time.Duration
	Token                      string
	DryRun                     bool
	ContainerRegistryTransport http.RoundTripper
	VersionMatch               *regexp.Regexp
	GithubClient               *github.Client
	Logger                     logr.Logger
}

type PackageVersion struct {
	PackageName string
	Version     string
	ID          int64
}

func (a *RetentionManager) Run(ctx context.Context) ([]*PackageVersion, error) {
	var removed []*PackageVersion
	toDelete := make(chan *PackageVersion)
	wg, ctx := errgroup.WithContext(ctx)

	wg.Go(func() error {
		defer close(toDelete)

		for _, packageName := range a.PackageNames {
			if err := a.findPackages(ctx, packageName, toDelete); err != nil {
				return err
			}
		}

		return nil
	})

	wg.Go(func() error {
		r, err := a.deletePackages(ctx, toDelete)
		removed = append(removed, r...)
		return err
	})

	err := wg.Wait()
	return removed, err
}

func (a *RetentionManager) findPackages(ctx context.Context, packageName string, toDelete chan *PackageVersion) error {
	versions, err := a.getAllVersionsForPackage(ctx, packageName)
	if err != nil {
		return err
	}

	packages := make(map[string]*github.PackageVersion)
	var references []string

	for _, version := range versions {
		a.Logger.Info("checking package version", "package", packageName, "version", *version.Name, "id", *version.ID)
		packages[*version.Name] = version

		if a.VersionMatch != nil {
			switch a.PackageType {
			case "container":
				if !a.matchContainer(version) {
					a.Logger.V(1).Info("skip package version as version does not match the required match regex", "package", packageName, "version", *version.Name, "id", *version.ID)
					continue
				}

				if version.Metadata.Container != nil && len(version.Metadata.Container.Tags) > 0 {
					tags, err := a.garbageCollectManifests(ctx, packageName, version)
					if err != nil {
						return err
					}

					references = append(references, tags...)
				}
			default:
				if !a.VersionMatch.MatchString(*version.Name) {
					a.Logger.V(1).Info("skip package version as version does not match the required match regex", "package", packageName, "version", *version.Name, "id", *version.ID)
					continue
				}
			}
		}

		if version.UpdatedAt == nil {
			a.Logger.V(1).Info("skip package version as no update timestamp exists", "package", packageName, "version", *version.Name, "id", *version.ID)
			continue
		}

		if a.Age != 0 {
			if version.UpdatedAt.Time.Add(a.Age).After(time.Now()) {
				a.Logger.V(1).Info("skip package version as age is too new", "package", packageName, "version", *version.Name, "id", *version.ID, "age", version.UpdatedAt)
				continue
			}
		}

		a.Logger.Info("package elected for deletion", "package", packageName, "version", *version.Name, "id", *version.ID)

		select {
		case toDelete <- &PackageVersion{
			Version:     *version.Name,
			PackageName: packageName,
			ID:          *version.ID,
		}:
		case <-ctx.Done():
			return ctx.Err()
		}

	}

	for _, reference := range references {
		if pv, ok := packages[reference]; ok {
			if a.Age != 0 {
				if pv.UpdatedAt.Time.Add(a.Age).After(time.Now()) {
					continue
				}
			}

			select {
			case toDelete <- &PackageVersion{
				Version:     *pv.Name,
				PackageName: packageName,
				ID:          *pv.ID,
			}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return nil
}

func (a *RetentionManager) garbageCollectManifests(ctx context.Context, packageName string, packageVersion *github.PackageVersion) ([]string, error) {
	var tags []string
	tagName := packageVersion.Metadata.Container.Tags[0]
	imageRef, err := name.ParseReference(fmt.Sprintf("ghcr.io/%s/%s:%s", a.OrganizationName, packageName, tagName))
	if err != nil {
		return tags, err
	}

	opts := []remote.Option{
		remote.WithAuth(&authn.Basic{
			Username: "ghcr",
			Password: a.Token,
		}),
		remote.WithTransport(a.ContainerRegistryTransport),
	}

	descriptor, err := remote.Head(imageRef, opts...)

	if err != nil {
		return tags, err
	}

	if descriptor.MediaType != types.OCIImageIndex {
		return tags, nil
	}

	index, err := remote.Index(imageRef, opts...)

	if err != nil {
		return tags, err
	}

	manifest, err := index.IndexManifest()
	if err != nil {
		return tags, err
	}

	for _, descriptor := range manifest.Manifests {
		tags = append(tags, descriptor.Digest.String())
	}

	return tags, nil
}

func (a *RetentionManager) deletePackages(ctx context.Context, toDelete chan *PackageVersion) ([]*PackageVersion, error) {
	var deleted []*PackageVersion
	for packageVersion := range toDelete {
		a.Logger.Info("deleting package version", "package", packageVersion.PackageName, "version", packageVersion.Version, "id", packageVersion.ID)

		if a.DryRun {
			continue
		}

		_, err := a.GithubClient.Organizations.PackageDeleteVersion(ctx, a.OrganizationName, a.PackageType, url.PathEscape(packageVersion.PackageName), packageVersion.ID)
		if err != nil {
			return deleted, err
		}

		deleted = append(deleted, packageVersion)
	}

	return deleted, nil
}
func (a *RetentionManager) matchContainer(version *github.PackageVersion) bool {
	if version.Metadata == nil || version.Metadata.Container.Tags == nil {
		return false
	}

	for _, tagName := range version.Metadata.Container.Tags {
		if a.VersionMatch.MatchString(tagName) {
			return true
		}
	}

	return false
}

func (a *RetentionManager) getAllVersionsForPackage(ctx context.Context, packageName string) ([]*github.PackageVersion, error) {
	var packageVersions []*github.PackageVersion
	opts := &github.PackageListOptions{}

	for {
		versions, resp, err := a.GithubClient.Organizations.PackageGetAllVersions(ctx, a.OrganizationName, a.PackageType, url.PathEscape(packageName), opts)
		if err != nil {
			return packageVersions, err
		}

		packageVersions = append(packageVersions, versions...)

		if resp.NextPage == 0 {
			break
		}

		opts.Page = resp.NextPage

	}

	return packageVersions, nil

}
