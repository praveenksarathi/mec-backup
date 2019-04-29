package proxy

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	proxyNet "github.com/linkerd/linkerd2-proxy-api/go/net"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
	"github.com/linkerd/linkerd2/controller/gen/controller/discovery"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/addr"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type collectListener struct {
	context context.Context
	stopCh  chan struct{}
}

func (c *collectListener) ClientClose() <-chan struct{} {
	return c.context.Done()
}

func (c *collectListener) ServerClose() <-chan struct{} {
	return c.stopCh
}

func (c *collectListener) Stop() {
	close(c.stopCh)
}

// implements the endpointUpdateListener interface
type collectUpdateListener struct {
	collectListener
	added             []*updateAddress
	removed           []*updateAddress
	noEndpointsCalled bool
	noEndpointsExists bool
}

func (c *collectUpdateListener) Update(add, remove []*updateAddress) {
	c.added = append(c.added, add...)
	c.removed = append(c.removed, remove...)
}

func (c *collectUpdateListener) NoEndpoints(exists bool) {
	c.noEndpointsCalled = true
	c.noEndpointsExists = exists
}

func (c *collectUpdateListener) SetServiceID(id *serviceID) {}

func newCollectUpdateListener() (*collectUpdateListener, context.CancelFunc) {
	ctx, cancelFn := context.WithCancel(context.Background())
	return &collectUpdateListener{collectListener: collectListener{context: ctx}}, cancelFn
}

// implements the profileUpdateListener interface
type collectProfileListener struct {
	collectListener
	profiles []*sp.ServiceProfile
}

func (c *collectProfileListener) Update(profile *sp.ServiceProfile) {
	c.profiles = append(c.profiles, profile)
}

func newCollectProfileListener() (*collectProfileListener, context.CancelFunc) {
	ctx, cancelFn := context.WithCancel(context.Background())
	return &collectProfileListener{collectListener: collectListener{context: ctx}}, cancelFn
}

// validate whether two servicePorts structs are equal
func equalServicePorts(servicePorts1, servicePorts2 servicePorts) error {
	if len(servicePorts1) != len(servicePorts2) {
		return fmt.Errorf("servicePorts length mismatch: [%+v] != [%+v]", servicePorts1, servicePorts2)
	}

	for serviceID := range servicePorts1 {
		portMap1 := servicePorts1[serviceID]
		portMap2 := servicePorts2[serviceID]
		if len(portMap1) != len(portMap2) {
			return fmt.Errorf("servicePorts portMap length mismatch: [%+v] != [%+v]", portMap1, portMap2)
		}

		for port := range portMap1 {
			sp1 := portMap1[port]
			sp2 := portMap2[port]

			if sp1.port != sp2.port {
				return fmt.Errorf("servicePort port mismatch: [%+v] != [%+v]", sp1.port, sp2.port)
			}
			if sp1.targetPort != sp2.targetPort {
				return fmt.Errorf("servicePort targetPort mismatch: [%+v] != [%+v]", sp1.targetPort, sp2.targetPort)
			}
			if sp1.service != sp2.service {
				return fmt.Errorf("servicePort service mismatch: [%+v] != [%+v]", sp1.service, sp2.service)
			}
			if len(sp1.addresses) != len(sp2.addresses) {
				return fmt.Errorf("servicePort addresses mismatch: [%+v] != [%+v]", sp1.addresses, sp2.addresses)
			}
			if sp1.endpoints.String() != sp2.endpoints.String() {
				return fmt.Errorf("servicePort endpoints mismatch: [%s] != [%s]", sp1.endpoints, sp2.endpoints)
			}
			for i := range sp1.addresses {
				if sp1.addresses[i].String() != sp2.addresses[i].String() {
					return fmt.Errorf("servicePort address mismatch: [%s] != [%s]", sp1.addresses[i], sp2.addresses[i])
				}
			}
		}
	}

	return nil
}

func makeUpdateAddress(ipStr string, portNum uint32, ns string, name string) *updateAddress {
	ip, _ := addr.ParseProxyIPV4(ipStr)
	return &updateAddress{
		address: &proxyNet.TcpAddress{Ip: ip, Port: portNum},
		pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns,
				Name:      name,
			},
		},
	}
}

// InitFakeDiscoveryServer takes a Kubernetes API client and returns a fake
// discovery API client, gRPC Server, and gRPC client connection.
// The caller is responsible for calling Server.GracefulStop() and
// ClientConn.Close().
func InitFakeDiscoveryServer(t *testing.T, k8sAPI *k8s.API) (discovery.DiscoveryClient, *grpc.Server, *grpc.ClientConn) {
	k8sAPI, err := k8s.NewFakeAPI("")
	if err != nil {
		t.Fatalf("NewFakeAPI returned an error: %s", err)
	}
	k8sAPI.Sync()

	lis := bufconn.Listen(1024 * 1024)
	gRPCServer, err := NewServer(
		"fake-addr", "", "controller-ns",
		false, false, false, k8sAPI, nil,
	)
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}
	go func() { gRPCServer.Serve(lis) }()

	proxyAPIConn, err := grpc.Dial(
		"fake-buf-addr",
		grpc.WithDialer(
			func(string, time.Duration) (net.Conn, error) {
				return lis.Dial()
			},
		),
		grpc.WithInsecure(),
	)
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}

	discoveryClient := discovery.NewDiscoveryClient(proxyAPIConn)

	return discoveryClient, gRPCServer, proxyAPIConn
}
