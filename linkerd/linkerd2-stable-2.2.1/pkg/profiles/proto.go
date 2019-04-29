package profiles

import (
	"fmt"
	"io"
	"net/http"
	"regexp"

	"github.com/emicklei/proto"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RenderProto reads a protobuf definition file and renders the corresponding
// ServiceProfile to a buffer, given a namespace, service, and control plane
// namespace.
func RenderProto(fileName, namespace, name string, w io.Writer) error {
	input, err := readFile(fileName)
	if err != nil {
		return err
	}

	parser := proto.NewParser(input)

	profile, err := protoToServiceProfile(parser, namespace, name)
	if err != nil {
		return err
	}

	return writeProfile(*profile, w)
}

func protoToServiceProfile(parser *proto.Parser, namespace, name string) (*sp.ServiceProfile, error) {
	definition, err := parser.Parse()
	if err != nil {
		return nil, err
	}

	routes := make([]*sp.RouteSpec, 0)
	pkg := ""

	handle := func(visitee proto.Visitee) {
		switch typed := visitee.(type) {
		case *proto.Package:
			pkg = typed.Name
		case *proto.RPC:
			if service, ok := typed.Parent.(*proto.Service); ok {
				route := &sp.RouteSpec{
					Name: typed.Name,
					Condition: &sp.RequestMatch{
						Method:    http.MethodPost,
						PathRegex: regexp.QuoteMeta(fmt.Sprintf("/%s.%s/%s", pkg, service.Name, typed.Name)),
					},
				}
				routes = append(routes, route)
			}
		}
	}

	proto.Walk(definition, handle)

	return &sp.ServiceProfile{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      fmt.Sprintf("%s.%s.svc.cluster.local", name, namespace),
			Namespace: namespace,
		},
		TypeMeta: ServiceProfileMeta,
		Spec: sp.ServiceProfileSpec{
			Routes: routes,
		},
	}, nil
}
