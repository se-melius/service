package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/zoenion/service/connection"
	pb "github.com/zoenion/service/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func (box *Box) ServiceAddress(name string) (string, error) {
	box.serverMutex.Lock()
	defer box.serverMutex.Unlock()

	s, exists := box.services[name]
	if !exists {
		return "", errors.New("not found")
	}
	return s.Address, nil
}

func GRPCConnectionDialer(ctx context.Context, serviceType pb.Type) (connection.Dialer, error) {
	reg := Registry(ctx)
	if reg == nil {
		return nil, errors.New("no registry configured")
	}

	infos, err := reg.GetOfType(serviceType)
	if err != nil {
		return nil, err
	}

	if len(infos) == 0 {
		return nil, errors.New("not found")
	}

	for _, info := range infos {
		for _, node := range info.Nodes {
			tlsConf := ClientTLSConfig(ctx)
			if tlsConf == nil {
				return connection.NewDialer(node.Address), nil
			} else {
				return connection.NewDialer(node.Address, grpc.WithTransportCredentials(credentials.NewTLS(tlsConf))), nil
			}
		}
	}
	return nil, fmt.Errorf("no service of type %s that supports gRPC has been found", serviceType)
}
