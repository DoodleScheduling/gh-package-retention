package action

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/google/go-github/v53/github"
	"github.com/sethvargo/go-githubactions"
	"golang.org/x/oauth2"
	"golang.org/x/sync/errgroup"
)

type Action struct {
	OrganizationName           string
	PackageType                string
	PackageName                string
	Age                        *time.Duration
	Token                      string
	DryRun                     bool
	ContainerRegistryTransport http.RoundTripper
	VersionMatch               *regexp.Regexp
	Action                     *githubactions.Action
	GithubClient               *github.Client
	SemanticReleaseCommand     *exec.Cmd
	Logger                     *log.Logger
}

func NewFromInputs(ctx context.Context, action *githubactions.Action) (*Action, error) {
	token := githubactions.GetInput("token")
	if token == "" {
		return nil, errors.New("missing parameter 'token'")
	}
	_ = os.Setenv("GITHUB_TOKEN", token)

	packageName := githubactions.GetInput("package-name")
	if packageName == "" {
		return nil, errors.New("missing parameter 'package_name'")
	}

	age := githubactions.GetInput("age")
	versionMatch := githubactions.GetInput("version-match")

	if versionMatch == "" && age == "" {
		return nil, errors.New("neither parameter 'age' nor 'version-match' set")
	}

	var versionMatchRegexp *regexp.Regexp
	if versionMatch != "" {
		r, err := regexp.Compile(versionMatch)
		if err != nil {
			return nil, err
		}

		versionMatchRegexp = r
	}

	var ageDuration *time.Duration
	if age != "" {
		a, err := time.ParseDuration(age)
		if err != nil {
			return nil, err
		}

		ageDuration = &a
	}

	organizationName := strings.ToLower(githubactions.GetInput("organization-name"))
	if organizationName == "" {
		return nil, errors.New("missing parameter 'organization-name'")
	}

	packageType := githubactions.GetInput("package-type")
	if packageType == "" {
		return nil, errors.New("missing parameter 'package-type'")
	}

	dryRun := false
	if dryRunInput := githubactions.GetInput("dry-run"); dryRunInput != "" {
		dryRunParsed, err := strconv.ParseBool(dryRunInput)
		if err != nil {
			return nil, fmt.Errorf("invalid type for parameter 'dry-run' provided: %w", err)
		}

		dryRun = dryRunParsed
	}

	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)

	tc := oauth2.NewClient(ctx, ts)
	ghClient := github.NewClient(tc)
	a := Action{
		OrganizationName:           organizationName,
		Token:                      token,
		PackageName:                packageName,
		VersionMatch:               versionMatchRegexp,
		ContainerRegistryTransport: http.DefaultTransport,
		Age:                        ageDuration,
		PackageType:                packageType,
		DryRun:                     dryRun,
		GithubClient:               ghClient,
		Action:                     action,
		Logger:                     log.New(os.Stdout, "", 0),
	}

	return &a, nil
}

func (a *Action) Run(ctx context.Context) error {
	toDelete := make(chan *github.PackageVersion)
	wg, ctx := errgroup.WithContext(ctx)

	wg.Go(func() error {
		defer close(toDelete)
		e := a.findPackages(ctx, toDelete)
		return e
	})

	wg.Go(func() error {
		packages, err := a.deletePackages(ctx, toDelete)
		if err != nil {
			return err
		}

		var names []string
		for _, version := range packages {
			names = append(names, *version.Name)
		}

		a.Action.SetOutput("versions", strings.Join(names, ","))
		return nil
	})

	return wg.Wait()
}

func (a *Action) findPackages(ctx context.Context, toDelete chan *github.PackageVersion) error {
	versions, err := a.getAllVersionsForPackage(ctx)
	if err != nil {
		return err
	}

	packages := make(map[string]*github.PackageVersion)
	var references []string

	for _, version := range versions {
		a.Logger.Printf("checking package version %s:%d", *version.Name, *version.ID)
		packages[*version.Name] = version

		if a.VersionMatch != nil {
			switch a.PackageType {
			case "container":
				if !a.matchContainer(version) {
					continue
				}

				if version.Metadata.Container != nil && len(version.Metadata.Container.Tags) > 0 {
					tags, err := a.garbageCollectManifests(ctx, version)
					if err != nil {
						return err
					}

					references = append(references, tags...)
				}
			default:
				if !a.VersionMatch.MatchString(*version.Name) {
					continue
				}
			}
		}

		if version.UpdatedAt == nil {
			continue
		}

		if a.Age != nil {
			if version.UpdatedAt.Time.Add(*a.Age).After(time.Now()) {
				continue
			}
		}

		a.Logger.Printf("package %s:%d elected for deletion", *version.Name, *version.ID)

		select {
		case toDelete <- version:
		case <-ctx.Done():
			return ctx.Err()
		}

	}

	for _, reference := range references {
		if packageVersion, ok := packages[reference]; ok {
			if a.Age != nil {
				if packageVersion.UpdatedAt.Time.Add(*a.Age).After(time.Now()) {
					continue
				}
			}

			select {
			case toDelete <- packageVersion:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return nil
}

func (a *Action) garbageCollectManifests(ctx context.Context, packageVersion *github.PackageVersion) ([]string, error) {
	var tags []string
	tagName := packageVersion.Metadata.Container.Tags[0]
	imageRef, err := name.ParseReference(fmt.Sprintf("ghcr.io/%s/%s:%s", a.OrganizationName, a.PackageName, tagName))
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

func (a *Action) deletePackages(ctx context.Context, toDelete chan *github.PackageVersion) ([]*github.PackageVersion, error) {
	var deleted []*github.PackageVersion
	for version := range toDelete {
		a.Logger.Printf("deleting package %s:%d (dryrun=%v)", *version.Name, *version.ID, a.DryRun)

		if a.DryRun {
			continue
		}

		_, err := a.GithubClient.Organizations.PackageDeleteVersion(ctx, a.OrganizationName, a.PackageType, url.PathEscape(a.PackageName), *version.ID)
		if err != nil {
			return deleted, err
		}

		deleted = append(deleted, version)
	}

	return deleted, nil
}

func (a *Action) matchContainer(version *github.PackageVersion) bool {
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

func (a *Action) getAllVersionsForPackage(ctx context.Context) ([]*github.PackageVersion, error) {
	var packageVersions []*github.PackageVersion
	opts := &github.PackageListOptions{}

	for {
		versions, resp, err := a.GithubClient.Organizations.PackageGetAllVersions(ctx, a.OrganizationName, a.PackageType, url.PathEscape(a.PackageName), opts)
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
