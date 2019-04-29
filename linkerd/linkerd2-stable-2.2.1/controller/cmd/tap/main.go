package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/controller/tap"
	"github.com/linkerd/linkerd2/pkg/admin"
	"github.com/linkerd/linkerd2/pkg/flags"
	log "github.com/sirupsen/logrus"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8088", "address to serve on")
	metricsAddr := flag.String("metrics-addr", ":9998", "address to serve scrapable metrics on")
	kubeConfigPath := flag.String("kubeconfig", "", "path to kube config")
	controllerNamespace := flag.String("controller-namespace", "linkerd", "namespace in which Linkerd is installed")
	singleNamespace := flag.Bool("single-namespace", false, "only operate in the controller namespace")
	tapPort := flag.Uint("tap-port", 4190, "proxy tap port to connect to")
	flags.ConfigureAndParse()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	k8sClient, err := k8s.NewClientSet(*kubeConfigPath)
	if err != nil {
		log.Fatalf("failed to create Kubernetes client: %s", err)
	}
	restrictToNamespace := ""
	if *singleNamespace {
		restrictToNamespace = *controllerNamespace
	}
	k8sAPI := k8s.NewAPI(
		k8sClient,
		nil,
		restrictToNamespace,
		k8s.DS,
		k8s.SS,
		k8s.Deploy,
		k8s.Pod,
		k8s.RC,
		k8s.Svc,
		k8s.RS,
	)

	server, lis, err := tap.NewServer(*addr, *tapPort, *controllerNamespace, k8sAPI)
	if err != nil {
		log.Fatal(err.Error())
	}

	k8sAPI.Sync() // blocks until caches are synced

	go func() {
		log.Println("starting gRPC server on", *addr)
		server.Serve(lis)
	}()

	go admin.StartServer(*metricsAddr)

	<-stop

	log.Println("shutting down gRPC server on", *addr)
	server.GracefulStop()
}
