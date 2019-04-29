package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/linkerd/linkerd2/controller/api/public"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	k8sResource "k8s.io/apimachinery/pkg/api/resource"
)

const (
	defaultNamespace = "linkerd"
	lineWidth        = 80
)

var (
	// special handling for Windows, on all other platforms these resolve to
	// os.Stdout and os.Stderr, thanks to https://github.com/mattn/go-colorable
	stdout = color.Output
	stderr = color.Error

	okStatus   = color.New(color.FgGreen, color.Bold).SprintFunc()("\u221A")  // √
	warnStatus = color.New(color.FgYellow, color.Bold).SprintFunc()("\u203C") // ‼
	failStatus = color.New(color.FgRed, color.Bold).SprintFunc()("\u00D7")    // ×

	controlPlaneNamespace string
	apiAddr               string // An empty value means "use the Kubernetes configuration"
	kubeconfigPath        string
	kubeContext           string
	verbose               bool

	// These regexs are not as strict as they could be, but are a quick and dirty
	// sanity check against illegal characters.
	alphaNumDash              = regexp.MustCompile("^[a-zA-Z0-9-]+$")
	alphaNumDashDot           = regexp.MustCompile("^[\\.a-zA-Z0-9-]+$")
	alphaNumDashDotSlashColon = regexp.MustCompile("^[\\./a-zA-Z0-9-:]+$")
)

// RootCmd represents the root Cobra command
var RootCmd = &cobra.Command{
	Use:   "linkerd",
	Short: "linkerd manages the Linkerd service mesh",
	Long:  `linkerd manages the Linkerd service mesh.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// enable / disable logging
		if verbose {
			log.SetLevel(log.DebugLevel)
		} else {
			log.SetLevel(log.PanicLevel)
		}

		controlPlaneNamespaceFromEnv := os.Getenv("LINKERD_NAMESPACE")
		if controlPlaneNamespace == defaultNamespace && controlPlaneNamespaceFromEnv != "" {
			controlPlaneNamespace = controlPlaneNamespaceFromEnv
		}

		if !alphaNumDash.MatchString(controlPlaneNamespace) {
			return fmt.Errorf("%s is not a valid namespace", controlPlaneNamespace)
		}

		return nil
	},
}

func init() {
	RootCmd.PersistentFlags().StringVarP(&controlPlaneNamespace, "linkerd-namespace", "l", defaultNamespace, "Namespace in which Linkerd is installed [$LINKERD_NAMESPACE]")
	RootCmd.PersistentFlags().StringVar(&kubeconfigPath, "kubeconfig", "", "Path to the kubeconfig file to use for CLI requests")
	RootCmd.PersistentFlags().StringVar(&kubeContext, "context", "", "Name of the kubeconfig context to use")
	RootCmd.PersistentFlags().StringVar(&apiAddr, "api-addr", "", "Override kubeconfig and communicate directly with the control plane at host:port (mostly for testing)")
	RootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Turn on debug logging")

	RootCmd.AddCommand(newCmdCheck())
	RootCmd.AddCommand(newCmdCompletion())
	RootCmd.AddCommand(newCmdDashboard())
	RootCmd.AddCommand(newCmdEndpoints())
	RootCmd.AddCommand(newCmdGet())
	RootCmd.AddCommand(newCmdInject())
	RootCmd.AddCommand(newCmdInstall())
	RootCmd.AddCommand(newCmdInstallCNIPlugin())
	RootCmd.AddCommand(newCmdInstallSP())
	RootCmd.AddCommand(newCmdLogs())
	RootCmd.AddCommand(newCmdProfile())
	RootCmd.AddCommand(newCmdRoutes())
	RootCmd.AddCommand(newCmdStat())
	RootCmd.AddCommand(newCmdTap())
	RootCmd.AddCommand(newCmdTop())
	RootCmd.AddCommand(newCmdUninject())
	RootCmd.AddCommand(newCmdVersion())
}

// cliPublicAPIClient builds a new public API client and executes default status
// checks to determine if the client can successfully perform cli commands. If the
// checks fail, then CLI will print an error and exit.
func cliPublicAPIClient() public.APIClient {
	return validatedPublicAPIClient(time.Time{}, false)
}

// validatedPublicAPIClient builds a new public API client and executes status
// checks to determine if the client can successfully connect to the API. If the
// checks fail, then CLI will print an error and exit. If the retryDeadline
// param is specified, then the CLI will print a message to stderr and retry.
func validatedPublicAPIClient(retryDeadline time.Time, apiChecks bool) public.APIClient {
	checks := []healthcheck.CategoryID{
		healthcheck.KubernetesAPIChecks,
		healthcheck.LinkerdControlPlaneExistenceChecks,
	}

	if apiChecks {
		checks = append(checks, healthcheck.LinkerdAPIChecks)
	}

	hc := healthcheck.NewHealthChecker(checks, &healthcheck.Options{
		ControlPlaneNamespace: controlPlaneNamespace,
		KubeConfig:            kubeconfigPath,
		KubeContext:           kubeContext,
		APIAddr:               apiAddr,
		RetryDeadline:         retryDeadline,
	})

	exitOnError := func(result *healthcheck.CheckResult) {
		if result.Retry {
			fmt.Fprintln(os.Stderr, "Waiting for control plane to become available")
			return
		}

		if result.Err != nil && !result.Warning {
			var msg string
			switch result.Category {
			case healthcheck.KubernetesAPIChecks:
				msg = "Cannot connect to Kubernetes"
			case healthcheck.LinkerdControlPlaneExistenceChecks:
				msg = "Cannot find Linkerd"
			case healthcheck.LinkerdAPIChecks:
				msg = "Cannot connect to Linkerd"
			}
			fmt.Fprintf(os.Stderr, "%s: %s\n", msg, result.Err)

			checkCmd := "linkerd check"
			if controlPlaneNamespace != defaultNamespace {
				checkCmd += fmt.Sprintf(" --linkerd-namespace %s", controlPlaneNamespace)
			}
			fmt.Fprintf(os.Stderr, "Validate the install with: %s\n", checkCmd)

			os.Exit(1)
		}
	}

	hc.RunChecks(exitOnError)
	return hc.PublicAPIClient()
}

type statOptionsBase struct {
	namespace    string
	timeWindow   string
	outputFormat string
}

func newStatOptionsBase() *statOptionsBase {
	return &statOptionsBase{
		namespace:    "default",
		timeWindow:   "1m",
		outputFormat: "",
	}
}

func (o *statOptionsBase) validateOutputFormat() error {
	switch o.outputFormat {
	case "table", "json", "":
		return nil
	default:
		return errors.New("--output currently only supports table and json")
	}
}

func renderStats(buffer bytes.Buffer, options *statOptionsBase) string {
	var out string
	switch options.outputFormat {
	case "json":
		out = string(buffer.Bytes())
	default:
		// strip left padding on the first column
		out = string(buffer.Bytes()[padding:])
		out = strings.Replace(out, "\n"+strings.Repeat(" ", padding), "\n", -1)
	}

	return out
}

// getRequestRate calculates request rate from Public API BasicStats.
func getRequestRate(success, failure uint64, timeWindow string) float64 {
	windowLength, err := time.ParseDuration(timeWindow)
	if err != nil {
		log.Error(err.Error())
		return 0.0
	}
	return float64(success+failure) / windowLength.Seconds()
}

// getSuccessRate calculates success rate from Public API BasicStats.
func getSuccessRate(success, failure uint64) float64 {
	if success+failure == 0 {
		return 0.0
	}
	return float64(success) / float64(success+failure)
}

// getPercentTLS calculates the percent of traffic that is TLS, from Public API
// BasicStats.
func getPercentTLS(stats *pb.BasicStats) float64 {
	reqTotal := stats.SuccessCount + stats.FailureCount
	if reqTotal == 0 {
		return 0.0
	}
	return float64(stats.TlsRequestCount) / float64(reqTotal)
}

// proxyConfigOptions holds values for command line flags that apply to both the
// install and inject commands. All fields in this struct should have
// corresponding flags added in the addProxyConfigFlags func later in this file.
type proxyConfigOptions struct {
	linkerdVersion          string
	proxyImage              string
	initImage               string
	dockerRegistry          string
	imagePullPolicy         string
	inboundPort             uint
	outboundPort            uint
	ignoreInboundPorts      []uint
	ignoreOutboundPorts     []uint
	proxyUID                int64
	proxyLogLevel           string
	proxyAPIPort            uint
	proxyControlPort        uint
	proxyMetricsPort        uint
	proxyCPURequest         string
	proxyMemoryRequest      string
	tls                     string
	disableExternalProfiles bool
	noInitContainer         bool

	// proxyOutboundCapacity is a special case that's only used for injecting the
	// proxy into the control plane install, and as such it does not have a
	// corresponding command line flag.
	proxyOutboundCapacity map[string]uint
}

const (
	optionalTLS           = "optional"
	defaultDockerRegistry = "gcr.io/linkerd-io"
	defaultKeepaliveMs    = 10000
)

func newProxyConfigOptions() *proxyConfigOptions {
	return &proxyConfigOptions{
		linkerdVersion:          version.Version,
		proxyImage:              defaultDockerRegistry + "/proxy",
		initImage:               defaultDockerRegistry + "/proxy-init",
		dockerRegistry:          defaultDockerRegistry,
		imagePullPolicy:         "IfNotPresent",
		inboundPort:             4143,
		outboundPort:            4140,
		ignoreInboundPorts:      nil,
		ignoreOutboundPorts:     nil,
		proxyUID:                2102,
		proxyLogLevel:           "warn,linkerd2_proxy=info",
		proxyAPIPort:            8086,
		proxyControlPort:        4190,
		proxyMetricsPort:        4191,
		proxyCPURequest:         "",
		proxyMemoryRequest:      "",
		tls:                     "",
		disableExternalProfiles: false,
		noInitContainer:         false,
		proxyOutboundCapacity:   map[string]uint{},
	}
}

func (options *proxyConfigOptions) validate() error {
	if !alphaNumDashDot.MatchString(options.linkerdVersion) {
		return fmt.Errorf("%s is not a valid version", options.linkerdVersion)
	}

	if !alphaNumDashDotSlashColon.MatchString(options.dockerRegistry) {
		return fmt.Errorf("%s is not a valid Docker registry. The url can contain only letters, numbers, dash, dot, slash and colon", options.dockerRegistry)
	}

	if options.imagePullPolicy != "Always" && options.imagePullPolicy != "IfNotPresent" && options.imagePullPolicy != "Never" {
		return fmt.Errorf("--image-pull-policy must be one of: Always, IfNotPresent, Never")
	}

	if options.proxyCPURequest != "" {
		if _, err := k8sResource.ParseQuantity(options.proxyCPURequest); err != nil {
			return fmt.Errorf("Invalid cpu request '%s' for --proxy-cpu flag", options.proxyCPURequest)
		}
	}

	if options.proxyMemoryRequest != "" {
		if _, err := k8sResource.ParseQuantity(options.proxyMemoryRequest); err != nil {
			return fmt.Errorf("Invalid memory request '%s' for --proxy-memory flag", options.proxyMemoryRequest)
		}
	}

	if options.tls != "" && options.tls != optionalTLS {
		return fmt.Errorf("--tls must be blank or set to \"%s\"", optionalTLS)
	}

	return nil
}

func (options *proxyConfigOptions) enableTLS() bool {
	return options.tls == optionalTLS
}

func (options *proxyConfigOptions) taggedProxyImage() string {
	image := strings.Replace(options.proxyImage, defaultDockerRegistry, options.dockerRegistry, 1)
	return fmt.Sprintf("%s:%s", image, options.linkerdVersion)
}

func (options *proxyConfigOptions) taggedProxyInitImage() string {
	image := strings.Replace(options.initImage, defaultDockerRegistry, options.dockerRegistry, 1)
	return fmt.Sprintf("%s:%s", image, options.linkerdVersion)
}

// addProxyConfigFlags adds command line flags for all fields in the
// proxyConfigOptions struct. To keep things organized, the flags should be
// added in the order that they're defined in the proxyConfigOptions struct.
func addProxyConfigFlags(cmd *cobra.Command, options *proxyConfigOptions) {
	cmd.PersistentFlags().StringVarP(&options.linkerdVersion, "linkerd-version", "v", options.linkerdVersion, "Tag to be used for Linkerd images")
	cmd.PersistentFlags().StringVar(&options.proxyImage, "proxy-image", options.proxyImage, "Linkerd proxy container image name")
	cmd.PersistentFlags().StringVar(&options.initImage, "init-image", options.initImage, "Linkerd init container image name")
	cmd.PersistentFlags().StringVar(&options.dockerRegistry, "registry", options.dockerRegistry, "Docker registry to pull images from")
	cmd.PersistentFlags().StringVar(&options.imagePullPolicy, "image-pull-policy", options.imagePullPolicy, "Docker image pull policy")
	cmd.PersistentFlags().UintVar(&options.inboundPort, "inbound-port", options.inboundPort, "Proxy port to use for inbound traffic")
	cmd.PersistentFlags().UintVar(&options.outboundPort, "outbound-port", options.outboundPort, "Proxy port to use for outbound traffic")
	cmd.PersistentFlags().UintSliceVar(&options.ignoreInboundPorts, "skip-inbound-ports", options.ignoreInboundPorts, "Ports that should skip the proxy and send directly to the application")
	cmd.PersistentFlags().UintSliceVar(&options.ignoreOutboundPorts, "skip-outbound-ports", options.ignoreOutboundPorts, "Outbound ports that should skip the proxy")
	cmd.PersistentFlags().Int64Var(&options.proxyUID, "proxy-uid", options.proxyUID, "Run the proxy under this user ID")
	cmd.PersistentFlags().StringVar(&options.proxyLogLevel, "proxy-log-level", options.proxyLogLevel, "Log level for the proxy")
	cmd.PersistentFlags().UintVar(&options.proxyAPIPort, "api-port", options.proxyAPIPort, "Port where the Linkerd controller is running")
	cmd.PersistentFlags().UintVar(&options.proxyControlPort, "control-port", options.proxyControlPort, "Proxy port to use for control")
	cmd.PersistentFlags().UintVar(&options.proxyMetricsPort, "metrics-port", options.proxyMetricsPort, "Proxy port to serve metrics on")
	cmd.PersistentFlags().StringVar(&options.proxyCPURequest, "proxy-cpu", options.proxyCPURequest, "Amount of CPU units that the proxy sidecar requests")
	cmd.PersistentFlags().StringVar(&options.proxyMemoryRequest, "proxy-memory", options.proxyMemoryRequest, "Amount of Memory that the proxy sidecar requests")
	cmd.PersistentFlags().StringVar(&options.tls, "tls", options.tls, "Enable TLS; valid settings: \"optional\"")
	cmd.PersistentFlags().BoolVar(&options.disableExternalProfiles, "disable-external-profiles", options.disableExternalProfiles, "Disables service profiles for non-Kubernetes services")
	cmd.PersistentFlags().BoolVar(&options.noInitContainer, "linkerd-cni-enabled", options.noInitContainer, "Experimental: Omit the proxy-init container when injecting the proxy; requires the linkerd-cni plugin to already be installed")
	cmd.PersistentFlags().MarkHidden("linkerd-cni-enabled")
}
