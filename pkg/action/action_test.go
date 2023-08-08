package action

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v53/github"
	"github.com/migueleliasweb/go-github-mock/src/mock"
	"github.com/sethvargo/go-githubactions"
	"github.com/stretchr/testify/assert"
)

type runTest struct {
	name           string
	action         func() *Action
	expectsError   bool
	expectedOutput string
}

func TestRun(t *testing.T) {
	var tests = []runTest{
		{
			name:           "One package which is older than age is removed",
			expectedOutput: "versions<<_GitHubActionsFileCommandDelimeter_\npackage-1\n_GitHubActionsFileCommandDelimeter_\n",
			action: func() *Action {
				age := time.Second * 10
				packageName1 := "package-1"
				var packageID1 int64 = 1

				packageName2 := "package-2"
				var packageID2 int64 = 2

				return &Action{
					PackageName:                "mypackage",
					PackageType:                "container",
					VersionMatch:               nil,
					Age:                        &age,
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
			name:           "No package is removed if both are older newer than age",
			expectedOutput: "versions<<_GitHubActionsFileCommandDelimeter_\n\n_GitHubActionsFileCommandDelimeter_\n",
			action: func() *Action {
				age := time.Second * 10
				packageName1 := "package-1"
				var packageID1 int64 = 1

				packageName2 := "package-2"
				var packageID2 int64 = 2

				return &Action{
					PackageName:                "mypackage",
					PackageType:                "container",
					VersionMatch:               nil,
					Age:                        &age,
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
			name:           "No packages are removed if neither matches age nor VersionMatch",
			expectedOutput: "versions<<_GitHubActionsFileCommandDelimeter_\n\n_GitHubActionsFileCommandDelimeter_\n",
			action: func() *Action {
				age := time.Second * 10
				packageName1 := "package-1"
				var packageID1 int64 = 1

				packageName2 := "package-2"
				var packageID2 int64 = 2

				return &Action{
					PackageName:                "mypackage",
					PackageType:                "container",
					VersionMatch:               regexp.MustCompile(`package-2`),
					Age:                        &age,
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
			name:           "Package which matches VersionMatch but age is too new is not removed",
			expectedOutput: "versions<<_GitHubActionsFileCommandDelimeter_\n\n_GitHubActionsFileCommandDelimeter_\n",
			action: func() *Action {
				age := time.Second * 10
				packageName1 := "package-1"
				var packageID1 int64 = 1

				return &Action{
					PackageName:                "mypackage",
					PackageType:                "container",
					VersionMatch:               regexp.MustCompile(`package-1`),
					Age:                        &age,
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
			name:           "Packages which match VersionMatch and age are removed",
			expectedOutput: "versions<<_GitHubActionsFileCommandDelimeter_\npackage-2,package-3\n_GitHubActionsFileCommandDelimeter_\n",
			action: func() *Action {
				age := time.Second * 10
				packageName1 := "package-1"
				var packageID1 int64 = 1

				packageName2 := "package-2"
				var packageID2 int64 = 2

				packageName3 := "package-3"
				var packageID3 int64 = 3

				return &Action{
					PackageName:                "mypackage",
					PackageType:                "container",
					VersionMatch:               regexp.MustCompile(`package`),
					Age:                        &age,
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
			name:           "Referenced packages in a oci.index package are also removed",
			expectedOutput: "versions<<_GitHubActionsFileCommandDelimeter_\npackage-1,sha256:c131f961d7af9055d4ff68fad06e7e24c3ce0b971a99d700bc6ba4947b12da86\n_GitHubActionsFileCommandDelimeter_\n",
			action: func() *Action {
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

				age := time.Second * 10
				packageName1 := "package-1"
				var packageID1 int64 = 1

				packageName2 := "sha256:c131f961d7af9055d4ff68fad06e7e24c3ce0b971a99d700bc6ba4947b12da86"
				var packageID2 int64 = 2

				packageName3 := "sha256:b6e64b25771997b04f2cee5ee7a0f44886833a80d6e6e41e0c3f2696d253ee5f"
				var packageID3 int64 = 3

				return &Action{
					PackageName:                "mypackage",
					PackageType:                "container",
					VersionMatch:               regexp.MustCompile(`package`),
					Age:                        &age,
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
			f, err := os.CreateTemp("", "output")
			assert.NoError(t, err)
			defer os.Remove(f.Name())

			err = os.Setenv("GITHUB_OUTPUT", f.Name())
			assert.NoError(t, err)

			actionLog := bytes.NewBuffer(nil)
			action := githubactions.New(
				githubactions.WithWriter(actionLog),
			)

			a := test.action()
			a.Action = action
			a.Logger = log.New(os.Stderr, "", 0)

			err = a.Run(context.TODO())
			if !test.expectsError {
				if err != nil {
					fmt.Printf("err: %#v", err)
				}
				assert.NoError(t, err)
			}

			b, err := os.ReadFile(f.Name())
			assert.NoError(t, err)
			assert.Equal(t, test.expectedOutput, string(b))
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
