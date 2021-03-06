package registry

import (
	"context"
	"crypto/tls"
	"github.com/google/uuid"
	"github.com/zoenion/common"
	"github.com/zoenion/common/clone"
	"github.com/zoenion/common/errors"
	"github.com/zoenion/service/discovery"
	pb2 "github.com/zoenion/service/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"log"
	"sync"
	"time"
)

type RegistryEventHandler interface {
	Handle(*pb2.Event)
}

type SyncedClient struct {
	servicesLock sync.Mutex
	handlersLock sync.Mutex
	syncMutex    sync.Mutex

	services map[string]*pb2.Info
	client   pb2.RegistryClient

	tlsConfig     *tls.Config
	conn          *grpc.ClientConn
	serverAddress string
	eventHandlers map[string]discovery.RegistryEventHandler

	idGenerator discovery.IDGenerator

	stop    bool
	syncing bool
}

func (r *SyncedClient) Disconnect() error {
	r.stop = true
	r.disconnected()
	if r.conn != nil {
		return r.conn.Close()
	}
	return nil
}

func (r *SyncedClient) RegisterService(i *pb2.Info, action pb2.ActionOnRegisterExistingService) (string, error) {
	err := r.connect()
	if err != nil {
		return "", err
	}

	defer func() { go r.sync() }()

	rsp, err := r.client.Register(context.Background(), &pb2.RegisterRequest{Service: i, Action: action})
	if err != nil {
		log.Printf("[Registry]:\tCould not register %s: %s\n", i.Name, err)
		return "", err
	}
	log.Println("[Registry]:\tRegistered")
	return rsp.RegistryId, nil
}

func (r *SyncedClient) DeregisterService(id string, nodes ...string) error {
	err := r.connect()
	if err != nil {
		return err
	}
	defer func() { go r.sync() }()

	_, err = r.client.Deregister(context.Background(), &pb2.DeregisterRequest{RegistryId: id, Nodes: nodes})
	if err == nil {
		log.Println("[Registry]:\tDeregistered")
	}
	return err
}

func (r *SyncedClient) GetService(id string) (*pb2.Info, error) {
	return r.get(id), nil
}

func (r *SyncedClient) Certificate(id string) ([]byte, error) {
	r.servicesLock.Lock()
	defer r.servicesLock.Unlock()

	for _, s := range r.services {
		if id == r.idGenerator.GenerateID(s.Namespace, s.Name) {
			strCert, found := s.Meta[common.MetaServiceCertificate]
			if !found {
				return nil, errors.NotFound
			}
			return []byte(strCert), nil
		}
	}
	return nil, errors.NotFound
}

func (r *SyncedClient) ConnectionInfo(id string, protocol pb2.Protocol) (*pb2.ConnectionInfo, error) {
	r.servicesLock.Lock()
	defer r.servicesLock.Unlock()

	ci := new(pb2.ConnectionInfo)

	for _, s := range r.services {
		if id == r.idGenerator.GenerateID(s.Namespace, s.Name) {
			for _, n := range s.Nodes {
				if protocol == n.Protocol {
					ci.Address = n.Address
					strCert, found := s.Meta["certificate"]
					if !found {
						return ci, nil
					}
					ci.Certificate = []byte(strCert)
					return ci, nil
				}
			}
		}
	}
	return nil, errors.NotFound
}

func (r *SyncedClient) RegisterEventHandler(h discovery.RegistryEventHandler) string {
	r.handlersLock.Lock()
	defer r.handlersLock.Unlock()
	hid := uuid.New().String()
	r.eventHandlers[hid] = h
	return hid
}

func (r *SyncedClient) DeregisterEventHandler(hid string) {
	r.handlersLock.Lock()
	defer r.handlersLock.Unlock()
	delete(r.eventHandlers, hid)
}

func (r *SyncedClient) GetOfType(t pb2.Type) ([]*pb2.Info, error) {
	r.servicesLock.Lock()
	defer r.servicesLock.Unlock()

	var result []*pb2.Info
	for _, s := range r.services {
		if s.Type == t {
			c := clone.New(s)
			result = append(result, c.(*pb2.Info))
		}
	}
	return result, nil
}

func (r *SyncedClient) Stop() error {
	r.stop = true
	r.services = nil
	return r.conn.Close()
}

func (r *SyncedClient) publishEvent(e pb2.Event) {
	r.handlersLock.Lock()
	r.handlersLock.Unlock()

	for _, handler := range r.eventHandlers {
		handler.Handle(&e)
	}
}

func (r *SyncedClient) get(name string) *pb2.Info {
	r.servicesLock.Lock()
	defer r.servicesLock.Unlock()
	info := r.services[name]
	return clone.New(info).(*pb2.Info)
}

func (r *SyncedClient) ofNamespace(namespace string) []*pb2.Info {
	r.servicesLock.Lock()
	defer r.servicesLock.Unlock()
	var services []*pb2.Info
	for _, s := range r.services {
		if namespace == "" || s.Namespace == namespace {
			services = append(services, clone.New(s).(*pb2.Info))
		}
	}
	return services
}

func (r *SyncedClient) saveService(info *pb2.Info) {
	r.servicesLock.Lock()
	defer r.servicesLock.Unlock()
	r.services[r.idGenerator.GenerateID(info.Namespace, info.Name)] = info
}

func (r *SyncedClient) deleteService(name string) {
	r.servicesLock.Lock()
	defer r.servicesLock.Unlock()
	delete(r.services, name)
}

func (r *SyncedClient) deleteServiceNode(name string, node *pb2.Info) {
	r.servicesLock.Lock()
	defer r.servicesLock.Unlock()
	service, exists := r.services[name]
	if !exists {
		return
	}

	var newNodes []*pb2.Node
	for _, oldNode := range service.Nodes {
		if oldNode.Name != node.Name {
			newNodes = append(newNodes, oldNode)
		}
	}
	service.Nodes = newNodes
}

func (r *SyncedClient) connect() error {
	if r.conn != nil && r.conn.GetState() == connectivity.Ready {
		return nil
	}

	var opts []grpc.DialOption
	if r.tlsConfig != nil {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(r.tlsConfig)))
	} else {
		opts = append(opts, grpc.WithInsecure())
	}

	var err error
	r.conn, err = grpc.Dial(r.serverAddress, opts...)
	if err != nil {
		log.Printf("connection to registry server failed: %s\n", err)
		return err
	}
	r.client = pb2.NewRegistryClient(r.conn)
	return nil
}

func (r *SyncedClient) sync() {
	if r.isSyncing() {
		return
	}
	r.setSyncing()

	for !r.stop {
		err := r.connect()
		if err != nil {
			log.Printf("[Registry]:\tcould not initialize connection to server at %s: %s\n", r.serverAddress, err)
			time.After(time.Second * 2)
			continue
		}
		r.listen()
	}
}

func (r *SyncedClient) listen() {
	stream, err := r.client.Listen(context.Background(), &pb2.ListenRequest{})
	if err != nil {
		r.conn = nil
		log.Printf("[Registry]:\tcould not sync with registry server: %s\n", err)
		return
	}
	defer stream.CloseSend()

	log.Printf("[Registry]:\tconnected to %s\n", r.serverAddress)
	for _, info := range r.services {
		_, err := r.client.Register(context.Background(), &pb2.RegisterRequest{Service: info})
		if err != nil {
			log.Printf("[Registry]:\tCould not register %s: %s\n", info.Name, err)
			return
		} else {
			log.Printf("[Registry]:\tregistered %s\n", info.Name)
		}
	}

	for !r.stop {
		event, err := stream.Recv()
		if err != nil {
			log.Printf("[Registry]:\tcould not get event: %s\n", err)
			return
		}

		for _, h := range r.eventHandlers {
			go h.Handle(event)
		}

		log.Printf("[Registry]:\tevent -> %s: %s\n", event.Type.String(), event.Name)

		switch event.Type {
		case pb2.EventType_Updated, pb2.EventType_Registered:
			r.saveService(event.Info)

		case pb2.EventType_DeRegistered:
			r.deleteService(event.Name)

		case pb2.EventType_DeRegisteredNode:
			r.deleteServiceNode(event.Name, event.Info)
		}
	}
}

func (r *SyncedClient) isSyncing() bool {
	r.syncMutex.Lock()
	defer r.syncMutex.Unlock()
	return r.syncing
}

func (r *SyncedClient) setSyncing() {
	r.syncMutex.Lock()
	defer r.syncMutex.Unlock()
	r.syncing = true
}

func (r *SyncedClient) disconnected() {
	r.services = nil
}

func NewSyncedRegistryClient(server string, tlsConfig *tls.Config, generator discovery.IDGenerator) *SyncedClient {
	return &SyncedClient{
		services:      map[string]*pb2.Info{},
		tlsConfig:     tlsConfig,
		serverAddress: server,
		idGenerator:   generator,
		eventHandlers: map[string]discovery.RegistryEventHandler{},
	}
}
