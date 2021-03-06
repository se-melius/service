package server

import (
	"crypto/tls"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/zoenion/service/gateway"
	"github.com/zoenion/service/interceptors"
	pb "github.com/zoenion/service/proto"
	"google.golang.org/grpc"
	"net/http"
)

type GatewayServiceMappingParams struct {
	ForNode     string
	GRPCNodeID  string
	Port        int
	Tls         *tls.Config
	Binder      gateway.WireEndpointFunc
	ServiceType pb.Type
	Node        *pb.Node
}

type GatewayParams struct {
	MiddlewareList []mux.MiddlewareFunc
	Port           int
	ProvideRouter  func() *mux.Router
	Tls            *tls.Config
	ServiceType    pb.Type
	Node           *pb.Node
}

type ServiceParams struct {
	Port                int
	Tls                 *tls.Config
	Interceptor         interceptors.GRPC
	RegisterHandlerFunc func(*grpc.Server)
	ServiceType         pb.Type
	Meta                map[string]string
	Node                *pb.Node
}

type Service struct {
	Secure     bool
	Address    string
	RegistryID string
	Server     *grpc.Server
}

func (s *Service) Stop() {
	s.Server.Stop()
}

type Gateway struct {
	RegistryID string
	Scheme     string
	Address    string
	Server     *http.Server
}

func (g *Gateway) URL() string {
	return fmt.Sprintf("%s://%s", g.Scheme, g.Address)
}

func (g *Gateway) Stop() error {
	return g.Server.Shutdown(nil)
}
