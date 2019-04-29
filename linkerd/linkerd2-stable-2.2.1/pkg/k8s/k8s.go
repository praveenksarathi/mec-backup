package k8s

import (
	"fmt"
	"net/url"
	"strings"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// These constants are string representations of Kubernetes resource types.
const (
	All                   = "all"
	Authority             = "authority"
	DaemonSet             = "daemonset"
	Deployment            = "deployment"
	Job                   = "job"
	Namespace             = "namespace"
	Pod                   = "pod"
	ReplicationController = "replicationcontroller"
	ReplicaSet            = "replicaset"
	Service               = "service"
	ServiceProfile        = "serviceprofile"
	StatefulSet           = "statefulset"

	// special case k8s job label, to not conflict with Prometheus' job label
	l5dJob = "k8s_job"
)

// AllResources is a sorted list of all resources defined as constants above.
var AllResources = []string{
	Authority,
	DaemonSet,
	Deployment,
	Job,
	Namespace,
	Pod,
	ReplicationController,
	ReplicaSet,
	Service,
	ServiceProfile,
	StatefulSet,
}

// StatAllResourceTypes represents the resources to query in StatSummary when Resource.Type is "all"
var StatAllResourceTypes = []string{
	// TODO: add Namespace here to decrease queries from the web process
	DaemonSet,
	StatefulSet,
	Deployment,
	ReplicationController,
	Pod,
	Service,
	Authority,
}

func generateKubernetesAPIURLFor(serverURL, namespace, path string) (*url.URL, error) {
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("path must start with a /, got [%s]", path)
	}

	fullPath := "/api/v1/namespaces/" + namespace + path
	return generateKubernetesURL(serverURL, fullPath)
}

func generateKubernetesURL(serverURL, path string) (*url.URL, error) {
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("path must start with a /, got [%s]", path)
	}

	return url.Parse(strings.TrimSuffix(serverURL, "/") + path)
}

// GetConfig returns kubernetes config based on the current environment.
// If fpath is provided, loads configuration from that file. Otherwise,
// GetConfig uses default strategy to load configuration from $KUBECONFIG,
// .kube/config, or just returns in-cluster config.
func GetConfig(fpath, kubeContext string) (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if fpath != "" {
		rules.ExplicitPath = fpath
	}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: kubeContext}
	return clientcmd.
		NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).
		ClientConfig()
}

// CanonicalResourceNameFromFriendlyName returns a canonical name from common shorthands used in command line tools.
// This works based on https://github.com/kubernetes/kubernetes/blob/63ffb1995b292be0a1e9ebde6216b83fc79dd988/pkg/kubectl/kubectl.go#L39
// This also works for non-k8s resources, e.g. authorities
func CanonicalResourceNameFromFriendlyName(friendlyName string) (string, error) {
	switch friendlyName {
	case "au", "authority", "authorities":
		return Authority, nil
	case "ds", "daemonset", "daemonsets":
		return DaemonSet, nil
	case "deploy", "deployment", "deployments":
		return Deployment, nil
	case "job", "jobs":
		return Job, nil
	case "ns", "namespace", "namespaces":
		return Namespace, nil
	case "po", "pod", "pods":
		return Pod, nil
	case "rc", "replicationcontroller", "replicationcontrollers":
		return ReplicationController, nil
	case "rs", "replicaset", "replicasets":
		return ReplicaSet, nil
	case "svc", "service", "services":
		return Service, nil
	case "sp", "serviceprofile", "serviceprofiles":
		return ServiceProfile, nil
	case "sts", "statefulset", "statefulsets":
		return StatefulSet, nil
	case "all":
		return All, nil
	}

	return "", fmt.Errorf("cannot find Kubernetes canonical name from friendly name [%s]", friendlyName)
}

// ShortNameFromCanonicalResourceName returns the shortest name for a k8s canonical name.
// Essentially the reverse of CanonicalResourceNameFromFriendlyName
func ShortNameFromCanonicalResourceName(canonicalName string) string {
	switch canonicalName {
	case Authority:
		return "au"
	case DaemonSet:
		return "ds"
	case Deployment:
		return "deploy"
	case Job:
		return "job"
	case Namespace:
		return "ns"
	case Pod:
		return "po"
	case ReplicationController:
		return "rc"
	case ReplicaSet:
		return "rs"
	case Service:
		return "svc"
	case ServiceProfile:
		return "sp"
	case StatefulSet:
		return "sts"
	default:
		return ""
	}
}

// KindToL5DLabel converts a Kubernetes `kind` to a Linkerd label.
// For example:
//   `pod` -> `pod`
//   `job` -> `k8s_job`
func KindToL5DLabel(k8sKind string) string {
	if k8sKind == Job {
		return l5dJob
	}
	return k8sKind
}
