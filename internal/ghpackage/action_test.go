package ghpackage

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-github/v53/github"
	"github.com/migueleliasweb/go-github-mock/src/mock"
	"github.com/stretchr/testify/assert"
)

type runTest struct {
	name             string
	RetentionManager func() *RetentionManager
	expected         []*PackageVersion
}

func TestRun(t *testing.T) {
	var tests = []runTest{
		{
			name: "One package which is older than age is removed",
			expected: []*PackageVersion{
				{
					PackageName: "mypackage",
					Version:     "package-1",
					ID:          1,
				},
			},
			RetentionManager: func() *RetentionManager {
				var (
					packageName1       = "package-1"
					packageID1   int64 = 1
					packageName2       = "package-2"
					packageID2   int64 = 2
				)

				return &RetentionManager{
					PackageNames:               []string{"mypackage"},
					PackageType:                "container",
					VersionMatch:               nil,
					Age:                        time.Second * 10,
					OrganizationName:           "myorg",
					ContainerRegistryTransport: noIndexResponseTransport(),
					DryRun:                     false,
					GithubClient: github.NewClient(mock.NewMockedHTTPClient(
						mock.WithRequestMatch(
							mock.GetOrgsPackagesVersionsByOrgByPackageTypeByPackageName,
							[]*github.PackageVersion{
								{
									Name:      &packageName1,
									ID:        &packageID1,
									UpdatedAt: &github.Timestamp{Time: time.Now().Add(-60 * time.Second)},
								},
								{
									Name:      &packageName2,
									ID:        &packageID2,
									UpdatedAt: &github.Timestamp{Time: time.Now()},
								},
							},
						),
						mock.WithRequestMatch(
							mock.DeleteOrgsPackagesVersionsByOrgByPackageTypeByPackageNameByPackageVersionId,
							&github.PackageVersion{
								Name: &packageName1,
								ID:   &packageID1,
							},
						),
					)),
				}
			},
		},
		{
			name: "No package is removed if both are older newer than age",
			RetentionManager: func() *RetentionManager {
				var (
					packageName1       = "package-1"
					packageID1   int64 = 1
					packageName2       = "package-2"
					packageID2   int64 = 2
				)

				return &RetentionManager{
					PackageNames:               []string{"mypackage"},
					PackageType:                "container",
					VersionMatch:               nil,
					Age:                        time.Second * 10,
					OrganizationName:           "myorg",
					ContainerRegistryTransport: noIndexResponseTransport(),
					DryRun:                     false,
					GithubClient: github.NewClient(mock.NewMockedHTTPClient(
						mock.WithRequestMatch(
							mock.GetOrgsPackagesVersionsByOrgByPackageTypeByPackageName,
							[]*github.PackageVersion{
								{
									Name:      &packageName1,
									ID:        &packageID1,
									UpdatedAt: &github.Timestamp{Time: time.Now().Add(-5 * time.Second)},
								},
								{
									Name:      &packageName2,
									ID:        &packageID2,
									UpdatedAt: &github.Timestamp{Time: time.Now()},
								},
							},
						),
						mock.WithRequestMatch(
							mock.DeleteOrgsPackagesVersionsByOrgByPackageTypeByPackageNameByPackageVersionId,
							&github.PackageVersion{
								Name: &packageName1,
								ID:   &packageID1,
							},
						),
					)),
				}
			},
		},
		{
			name: "No packages are removed if neither matches age nor VersionMatch",
			RetentionManager: func() *RetentionManager {
				var (
					packageName1       = "package-1"
					packageID1   int64 = 1
					packageName2       = "package-2"
					packageID2   int64 = 2
				)

				return &RetentionManager{
					PackageNames:               []string{"mypackage"},
					PackageType:                "container",
					VersionMatch:               regexp.MustCompile(`package-2`),
					Age:                        time.Second * 10,
					OrganizationName:           "myorg",
					ContainerRegistryTransport: noIndexResponseTransport(),
					DryRun:                     false,
					GithubClient: github.NewClient(mock.NewMockedHTTPClient(
						mock.WithRequestMatch(
							mock.GetOrgsPackagesVersionsByOrgByPackageTypeByPackageName,
							[]*github.PackageVersion{
								{
									Name:      &packageName1,
									ID:        &packageID1,
									UpdatedAt: &github.Timestamp{Time: time.Now().Add(-5 * time.Second)},
								},
								{
									Name: &packageName2,
									ID:   &packageID2,
									Metadata: &github.PackageMetadata{
										Container: &github.PackageContainerMetadata{
											Tags: []string{"does-not-matcg"},
										},
									},
									UpdatedAt: &github.Timestamp{Time: time.Now()},
								},
							},
						),
					)),
				}
			},
		},
		{
			name: "Package which matches VersionMatch but age is too new is not removed",
			RetentionManager: func() *RetentionManager {
				var (
					packageName1       = "package-1"
					packageID1   int64 = 1
				)

				return &RetentionManager{
					PackageNames:               []string{"mypackage"},
					PackageType:                "container",
					VersionMatch:               regexp.MustCompile(`package-1`),
					Age:                        time.Second * 10,
					OrganizationName:           "myorg",
					ContainerRegistryTransport: noIndexResponseTransport(),
					DryRun:                     false,
					GithubClient: github.NewClient(mock.NewMockedHTTPClient(
						mock.WithRequestMatch(
							mock.GetOrgsPackagesVersionsByOrgByPackageTypeByPackageName,
							[]*github.PackageVersion{
								{
									Name: &packageName1,
									ID:   &packageID1,
									Metadata: &github.PackageMetadata{
										Container: &github.PackageContainerMetadata{
											Tags: []string{"does-not-match", "package-1"},
										},
									},
									UpdatedAt: &github.Timestamp{Time: time.Now()},
								},
							},
						),
					)),
				}
			},
		},
		{
			name: "Packages which match VersionMatch and age are removed",
			expected: []*PackageVersion{
				{
					PackageName: "mypackage",
					Version:     "package-2",
					ID:          2,
				},
				{
					PackageName: "mypackage",
					Version:     "package-3",
					ID:          3,
				},
			},
			RetentionManager: func() *RetentionManager {
				var (
					packageName1       = "package-1"
					packageID1   int64 = 1
					packageName2       = "package-2"
					packageID2   int64 = 2
					packageName3       = "package-3"
					packageID3   int64 = 3
				)

				return &RetentionManager{
					PackageNames:               []string{"mypackage"},
					PackageType:                "container",
					VersionMatch:               regexp.MustCompile(`package`),
					Age:                        time.Second * 10,
					OrganizationName:           "myorg",
					ContainerRegistryTransport: noIndexResponseTransport(),
					DryRun:                     false,
					GithubClient: github.NewClient(mock.NewMockedHTTPClient(
						mock.WithRequestMatch(
							mock.GetOrgsPackagesVersionsByOrgByPackageTypeByPackageName,
							[]*github.PackageVersion{
								{
									Name:      &packageName1,
									ID:        &packageID1,
									UpdatedAt: &github.Timestamp{Time: time.Now().Add(-5 * time.Second)},
								},
								{
									Name: &packageName2,
									ID:   &packageID2,
									Metadata: &github.PackageMetadata{
										Container: &github.PackageContainerMetadata{
											Tags: []string{"does-not-match", "package-2"},
										},
									},
									UpdatedAt: &github.Timestamp{Time: time.Now().Add(-60 * time.Second)},
								},
								{
									Name: &packageName3,
									ID:   &packageID3,
									Metadata: &github.PackageMetadata{
										Container: &github.PackageContainerMetadata{
											Tags: []string{"does-not-match", "package-3"},
										},
									},
									UpdatedAt: &github.Timestamp{Time: time.Now().Add(-60 * time.Second)},
								},
							},
						),
						mock.WithRequestMatch(
							mock.DeleteOrgsPackagesVersionsByOrgByPackageTypeByPackageNameByPackageVersionId,
							&github.PackageVersion{
								Name: &packageName2,
								ID:   &packageID2,
							},
							&github.PackageVersion{
								Name: &packageName3,
								ID:   &packageID3,
							},
						),
					)),
				}
			},
		},
		{
			name: "Referenced packages in a oci.index package are also removed",
			expected: []*PackageVersion{
				{
					PackageName: "mypackage",
					Version:     "package-1",
					ID:          1,
				},
				{
					PackageName: "mypackage",
					Version:     "sha256:c131f961d7af9055d4ff68fad06e7e24c3ce0b971a99d700bc6ba4947b12da86",
					ID:          2,
				},
			},
			RetentionManager: func() *RetentionManager {
				manifest := `{
						"mediaType": "application/vnd.oci.image.index.v1+json",
						"schemaVersion": 2,
						"manifests": [
						  {
							"mediaType": "application/vnd.oci.image.manifest.v1+json",
							"digest": "sha256:c131f961d7af9055d4ff68fad06e7e24c3ce0b971a99d700bc6ba4947b12da86",
							"size": 1055,
							"platform": {
							  "architecture": "amd64",
							  "os": "linux"
							}
						  },
						  {
							"mediaType": "application/vnd.oci.image.manifest.v1+json",
							"digest": "sha256:b6e64b25771997b04f2cee5ee7a0f44886833a80d6e6e41e0c3f2696d253ee5f",
							"size": 566,
							"annotations": {
							  "vnd.docker.reference.digest": "sha256:c131f961d7af9055d4ff68fad06e7e24c3ce0b971a99d700bc6ba4947b12da86",
							  "vnd.docker.reference.type": "attestation-manifest"
							},
							"platform": {
							  "architecture": "unknown",
							  "os": "unknown"
							}
						  }
						]
					  }`

				response := &http.Response{
					Header:     make(http.Header),
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(manifest)),
				}
				response.Header.Set("Content-Type", "application/vnd.oci.image.index.v1+json")
				response.Header.Set("Docker-Content-Digest", "sha256:a60d0af675b0bad03ebdb529ed1b6009604063136f30516568028008c221e62d")

				var (
					packageName1       = "package-1"
					packageID1   int64 = 1
					packageName2       = "sha256:c131f961d7af9055d4ff68fad06e7e24c3ce0b971a99d700bc6ba4947b12da86"
					packageID2   int64 = 2
					packageName3       = "sha256:b6e64b25771997b04f2cee5ee7a0f44886833a80d6e6e41e0c3f2696d253ee5f"
					packageID3   int64 = 3
				)

				return &RetentionManager{
					PackageNames:               []string{"mypackage"},
					PackageType:                "container",
					VersionMatch:               regexp.MustCompile(`package`),
					Age:                        time.Second * 10,
					OrganizationName:           "myorg",
					DryRun:                     false,
					ContainerRegistryTransport: newMockTransport(&http.Response{StatusCode: http.StatusOK}, response, &http.Response{StatusCode: http.StatusOK}, response, &http.Response{StatusCode: http.StatusOK}),
					GithubClient: github.NewClient(mock.NewMockedHTTPClient(
						mock.WithRequestMatch(
							mock.GetOrgsPackagesVersionsByOrgByPackageTypeByPackageName,
							[]*github.PackageVersion{
								{
									Name: &packageName1,
									ID:   &packageID1,
									Metadata: &github.PackageMetadata{
										Container: &github.PackageContainerMetadata{
											Tags: []string{"package-1-index"},
										},
									},
									UpdatedAt: &github.Timestamp{Time: time.Now().Add(-60 * time.Second)},
								},
								{
									Name:      &packageName2,
									ID:        &packageID2,
									UpdatedAt: &github.Timestamp{Time: time.Now().Add(-60 * time.Second)},
								},
								{
									Name:      &packageName3,
									ID:        &packageID3,
									UpdatedAt: &github.Timestamp{Time: time.Now().Add(-5 * time.Second)},
								},
							},
						),
						mock.WithRequestMatch(
							mock.DeleteOrgsPackagesVersionsByOrgByPackageTypeByPackageNameByPackageVersionId,
							&github.PackageVersion{
								Name: &packageName1,
								ID:   &packageID1,
							},
							&github.PackageVersion{
								Name: &packageName2,
								ID:   &packageID2,
							},
						),
					)),
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			a := test.RetentionManager()
			a.Logger = logr.Discard()

			remove, err := a.Run(context.TODO())
			assert.Equal(t, test.expected, remove)
			assert.NoError(t, err)
		})
	}
}

type mockTransport struct {
	responsePool []*http.Response
}

func newMockTransport(r ...*http.Response) http.RoundTripper {
	return &mockTransport{
		responsePool: r,
	}
}

func (t *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	v := t.responsePool[0]
	t.responsePool = t.responsePool[1:]
	return v, nil
}

func noIndexResponseTransport() http.RoundTripper {
	manifest := `{
		"mediaType": "application/vnd.oci.image.manifest.v1+json",
		"schemaVersion": 2,
		"config": {
			"mediaType": "application/vnd.oci.image.config.v1+json",
			"digest": "sha256:60a4eb0188d8c3859ff2d116bdbcd30af6503afcad8e2e1a16e0c26eed1917a7",
			"size": 3499
		},
		"layers": []
	  }`

	response := &http.Response{
		Header:     make(http.Header),
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(manifest)),
	}

	response.Header.Set("Content-Type", "application/json")
	response.Header.Set("Docker-Content-Digest", "sha256:a60d0af675b0bad03ebdb529ed1b6009604063136f30516568028008c221e62d")

	return newMockTransport(&http.Response{StatusCode: http.StatusOK}, response, &http.Response{StatusCode: http.StatusOK}, response, &http.Response{StatusCode: http.StatusOK}, response)
}
