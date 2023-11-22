package main

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/doodlescheduling/gh-package-retention/internal/ghpackage"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/google/go-github/v53/github"
	"github.com/sethvargo/go-envconfig"
	flag "github.com/spf13/pflag"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
)

type Config struct {
	Yes bool `env:"YES"`
	Log struct {
		Level    string `env:"LOG_LEVEL"`
		Encoding string `env:"LOG_ENCODING"`
	}
	VersionMatch string        `env:"VERSION_MATCH"`
	PackageType  string        `env:"PACKAGE_TYPE"`
	Packages     []string      `env:"PACKAGES"`
	Token        string        `env:"GITHUB_TOKEN"`
	Age          time.Duration `env:"AGE"`
	OrgName      string        `env:"ORG_NAME"`
}

var (
	config = &Config{}
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func init() {
	flag.BoolVar(&config.Yes, "yes", false, "Skip dry-run and delete packages")
	flag.StringVar(&config.VersionMatch, "version-match", "", "Version match")
	flag.DurationVar(&config.Age, "age", 0, "Max age of a package version. Package versions older than the specified age will be removed (As long as version-match macthes the version).")
	flag.StringVar(&config.OrgName, "org-name", "", "Github organization name which is the package owner")
	flag.StringVar(&config.Token, "token", "", "Github token (By default GITHUB_TOKEN will be used)")
	flag.StringVar(&config.PackageType, "package-type", "", "Type of package (container, maven, ...)")
	flag.StringVar(&config.Log.Encoding, "log-encoding", "console", "Log encoding format. Can be 'json' or 'console'.")
	flag.StringVar(&config.Log.Level, "log-level", "info", "Log verbosity level. Can be one of 'trace', 'debug', 'info', 'error'.")
}

func main() {
	ctx := context.Background()
	if err := envconfig.Process(ctx, config); err != nil {
		must(err)
	}

	flag.Parse()

	logger, err := buildLogger()
	must(err)

	if len(config.Packages) == 0 {
		must(errors.New("at least one package name must be given"))
	}

	var versionMatchRegexp *regexp.Regexp
	if config.VersionMatch != "" {
		r, err := regexp.Compile(config.VersionMatch)
		must(err)
		versionMatchRegexp = r
	}

	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: config.Token},
	)

	tc := oauth2.NewClient(ctx, ts)
	tc.Transport = &loggingRoundTripper{
		next:   tc.Transport,
		logger: logger,
	}

	containerTransport := &loggingRoundTripper{
		next:   http.DefaultTransport,
		logger: logger,
	}

	ghClient := github.NewClient(tc)

	a := ghpackage.RetentionManager{
		ContainerRegistryTransport: containerTransport,
		PackageType:                strings.ToLower(config.PackageType),
		Token:                      config.Token,
		DryRun:                     !config.Yes,
		GithubClient:               ghClient,
		PackageNames:               config.Packages,
		Age:                        config.Age,
		OrganizationName:           strings.ToLower(config.OrgName),
		VersionMatch:               versionMatchRegexp,
		Logger:                     logger,
	}

	_, err = a.Run(ctx)
	must(err)
}

func buildLogger() (logr.Logger, error) {
	logOpts := zap.NewDevelopmentConfig()
	logOpts.Encoding = config.Log.Encoding

	err := logOpts.Level.UnmarshalText([]byte(config.Log.Level))
	if err != nil {
		return logr.Discard(), err
	}

	zapLog, err := logOpts.Build()
	if err != nil {
		return logr.Discard(), err
	}

	return zapr.NewLogger(zapLog), nil
}

type loggingRoundTripper struct {
	logger logr.Logger
	next   http.RoundTripper
}

func (p loggingRoundTripper) RoundTrip(req *http.Request) (res *http.Response, e error) {
	p.logger.V(1).Info("http request sent", "method", req.Method, "uri", req.URL.String())
	res, err := p.next.RoundTrip(req)
	p.logger.V(1).Info("http response received", "method", req.Method, "uri", req.URL.String(), "status", res.StatusCode, "err", err)
	return res, err
}
