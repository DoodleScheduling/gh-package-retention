## ghcr.io package retention

[![release](https://img.shields.io/github/release/DoodleScheduling/gh-package-retention/all.svg)](https://github.com/DoodleScheduling/gh-package-retention/releases)
[![release](https://github.com/doodlescheduling/package-retention/actions/workflows/release.yaml/badge.svg)](https://github.com/doodlescheduling/package-retention/actions/workflows/release.yaml)
[![report](https://goreportcard.com/badge/github.com/DoodleScheduling/gh-package-retention)](https://goreportcard.com/report/github.com/DoodleScheduling/gh-package-retention)
[![Coverage Status](https://coveralls.io/repos/github/DoodleScheduling/gh-package-retention/badge.svg?branch=master)](https://coveralls.io/github/DoodleScheduling/gh-package-retention?branch=master)
[![license](https://img.shields.io/github/license/DoodleScheduling/gh-package-retention.svg)](https://github.com/DoodleScheduling/gh-package-retention/blob/master/LICENSE)

Package retention manager for ghcr.io.
Unlike other apps implementing a similar mechanism this one actually has support for docker manifests
and can do retentions for multi platform image tags.

* Delete packages based on age (and optionally in combination with a regex filter)
* Supports rentention for multi platform container images (and all other package types)

## Usage

This example will remove package versions older than 3000h from the maven repositories package and anotherpackage:

```
package-retention --org-name githuborgname --package-type maven --age 3000h package anotherpackage
```

## Installation

### Brew
```
brew tap doodlescheduling/gh-package-retention
brew install gh-package-retention
```

### Docker
```
docker pull ghcr.io/doodlescheduling/gh-package-retention:v2
```

### Github cli extenion
```
gh extension install DoodleScheduling/gh-package-retention
```

## Arguments

| Flag  | Env | Default | Description |
| ------------- | ------------- | ------------- | ------------- |
| ``  | `PACKAGES`  | `` | **REQUIRED**: One or more paths comma separated to kustomize |
| `--package-type` | `PACKAGE_TYPE` | `` | **REQUIRED**: Type of package (container, maven, ...) |
| `--org-name` | `ORG_NAME` | `` | **REQUIRED**: Github organization name which is the package owner |
| `--age`  | `AGE`  | `` | Max age of a package version. Package versions older than the specified age will be removed (As long as version-match macthes the version). |
| `--dry-run`  | `DRY_RUN` | `false` | Run in dry mode only |
| `--log-encoding`  | `LOG_ENCODING` | `console` | Log encoding format. Can be 'json' or 'console'. (default "console") |
| `--log-level`  | `LOG_LEVEL`  | `info` | Log verbosity level. Can be one of 'trace', 'debug', 'info', 'error'. (default "info") |
| `--token`  | `GITHUB_TOKEN` | `1.27.0` | Github token (By default GITHUB_TOKEN will be used) |
| `--version-match`  | `VERSION_MATCH` | `` | Regex to match a version. Note for containers it will match container tags (If package-type is container)' |


## Github Action

This app works also great on CI, in fact this was the original reason why it was created.

### Example usage


```yaml
name: package-retention
on:
  schedule:
    - cron: '0 0 * * *'
jobs:
  package-retention:
    permissions:
      packages: write
    runs-on: ubuntu-latest
    steps:
      - uses: doodlescheduling/gh-package-retention@v2
        name: Delete oci helm charts older than 90 days
        env:
          PACKAGES: charts/${{ github.event.repository.name }}
          PACKAGE_TYPE: container
          AGE: 2160h
          VERSION_MATCH: 0.0.0-.*
      - uses: doodlescheduling/gh-package-retention@v2
        name: Delete maven snapshot versions older than 90 days
        with:
          PACKAGES: org.example.mypackage
          PACKAGE_TYPE: maven
          AGE: 2160h
          VERSION_MATCH: ".*-SNAPSHOT$"
```
