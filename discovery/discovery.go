package discovery

import (
	pb2 "github.com/zoenion/service/proto"
)

type IDGenerator interface {
	GenerateID(namespace, name string) string
}

type idGeneratorFunc struct {
	f func(string, string) string
}

func (ig *idGeneratorFunc) GenerateID(namespace, name string) string {
	return ig.f(namespace, name)
}

func IDGeneratorFunc(generateFunc func(string, string) string) IDGenerator {
	return &idGeneratorFunc{f: generateFunc}
}

type RegistryEventHandler interface {
	Handle(*pb2.Event)
}

type eventHandlerFunc struct {
	f func(event *pb2.Event)
}

func (hf *eventHandlerFunc) Handle(event *pb2.Event) {
	hf.f(event)
}

type Registry interface {
	RegisterService(info *pb2.Info, action pb2.ActionOnRegisterExistingService) (string, error)
	DeregisterService(id string, nodes ...string) error
	GetService(id string) (*pb2.Info, error)
	Certificate(id string) ([]byte, error)
	ConnectionInfo(id string, protocol pb2.Protocol) (*pb2.ConnectionInfo, error)
	RegisterEventHandler(h RegistryEventHandler) string
	DeregisterEventHandler(string)
	GetOfType(t pb2.Type) ([]*pb2.Info, error)
	Stop() error
}
