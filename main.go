package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/doodlescheduling/package-retention/internal/ghpackage"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/google/go-github/v53/github"
	"github.com/ory/viper"
	flag "github.com/spf13/pflag"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
)

var (
	dryRun       bool
	versionMatch string
	packageType  string
	token        string
	age          time.Duration
	logLevel     string
	logEncoding  string
	orgName      string
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	flag.BoolVar(&dryRun, "dry-run", dryRun, "Run in dry mode only")
	flag.StringVar(&versionMatch, "version-match", versionMatch, "Version match")
	flag.DurationVar(&age, "age", age, "Max age of a package version. Package versions older than the specified age will be removed (As long as version-match macthes the version).")
	flag.StringVar(&orgName, "org-name", orgName, "Github organization name which is the package owner")
	flag.StringVar(&token, "token", token, "Github token (By default GITHUB_TOKEN will be used)")
	flag.StringVar(&packageType, "package-type", packageType, "Type of package (container, maven, ...)")
	flag.StringVar(&logEncoding, "log-encoding", "console", "Log encoding format. Can be 'json' or 'console'.")
	flag.StringVar(&logLevel, "log-level", "info", "Log verbosity level. Can be one of 'trace', 'debug', 'info', 'error'.")

	flag.Parse()

	// Import flags into viper and bind them to env vars
	// flags are converted to upper-case, - is replaced with _
	err := viper.BindPFlags(flag.CommandLine)
	must(err)

	replacer := strings.NewReplacer("-", "_")
	viper.SetEnvKeyReplacer(replacer)
	viper.AutomaticEnv()

	var log logr.Logger
	logOpts := zap.NewDevelopmentConfig()
	logOpts.Encoding = logEncoding

	err = logOpts.Level.UnmarshalText([]byte(logLevel))
	must(err)
	zapLog, err := logOpts.Build()
	must(err)
	log = zapr.NewLogger(zapLog)

	packages := flag.Args()
	if len(packages) == 0 {
		if os.Getenv("PACKAGES") != "" {
			packages = strings.Split(os.Getenv("PACKAGES"), ",")
		} else {
			must(errors.New("at least one package name must be given"))
		}
	}

	var versionMatchRegexp *regexp.Regexp
	if versionMatch != "" {
		r, err := regexp.Compile(viper.GetString("version-match"))
		must(err)

		versionMatchRegexp = r
	}

	token = viper.GetString("token")
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}

	ctx := context.TODO()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)

	tc := oauth2.NewClient(ctx, ts)
	tc.Transport = &loggingRoundTripper{
		next:   tc.Transport,
		logger: log,
	}

	ghClient := github.NewClient(tc)

	a := ghpackage.RetentionManager{
		PackageType:      viper.GetString("package-type"),
		Token:            token,
		DryRun:           viper.GetBool("dry-run"),
		GithubClient:     ghClient,
		PackageNames:     packages,
		Age:              viper.GetDuration("age"),
		OrganizationName: viper.GetString("org-name"),
		VersionMatch:     versionMatchRegexp,
		Logger:           log,
	}

	_, err = a.Run(ctx)
	must(err)
}

type loggingRoundTripper struct {
	logger logr.Logger
	next   http.RoundTripper
}

func (p loggingRoundTripper) RoundTrip(req *http.Request) (res *http.Response, e error) {
	p.logger.V(1).Info("http request sent", "method", req.Method, "uri", req.URL.String())
	res, err := p.next.RoundTrip(req)
	p.logger.V(1).Info("http response received", "method", req.Method, "uri", req.URL.String(), "status", res.StatusCode)
	return res, err
}
