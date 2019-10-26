package service

import (
	"crypto/tls"
	"fmt"
	crypto2 "github.com/zoenion/common/crypto"
	"github.com/zoenion/service/authentication"
	"github.com/zoenion/service/errors"
	"google.golang.org/grpc/credentials"
	"log"
	"strings"
)

func (box *Box) Init(opts ...InitOption) error {
	var err error
	options := &initOptions{}
	for _, opt := range opts {
		opt(options)
	}

	if box.params.CertificatePath != "" {
		err = box.loadCertificateKeyPairFromFiles()
		if err != nil {
			return errors.Errorf("could not load certificate/key pair from file: %s", err)
		}
	} else {
		if box.params.CA {
			err = box.loadOrGenerateCACertificateKeyPair()
			if err != nil {
				return errors.Errorf("could not load CA key pair: %s", err)
			}

		} else {
			err = box.loadCACredentials()
			if err != nil {
				return errors.Errorf("could not initialize CA credentials: %s", err)
			}

			err = box.loadOrGenerateCertificateKeyPair()
			if err != nil {
				return err
			}
		}
	}

	box.registry = options.registry
	if options.registry == nil {
		err = box.initRegistry()
		if err != nil {
			return errors.Errorf("could not initialize registry: %s", err)
		}
	}

	if box.params.CA {
		return box.startCA(options.credentialsProvider)
	}
	return nil
}

func (box *Box) loadCertificateKeyPairFromFiles() error {
	var err error
	box.cert, err = crypto2.LoadCertificate(box.params.CertificatePath)
	if err == nil {
		box.privateKey, err = crypto2.LoadPrivateKey(nil, box.params.KeyPath)
	}
	return err
}

func (box *Box) loadCACredentials() (err error) {
	if box.params.CACertPath == "" {
		return errors.New("missing CA certificate path parameter")
	}

	if box.params.CACredentials == "" {
		return errors.New("missing CA client login/password parameter")
	}

	box.caCert, err = crypto2.LoadCertificate(box.params.CACertPath)
	if err != nil {
		return
	}

	box.caGRPCTransportCredentials, err = credentials.NewClientTLSFromFile(box.params.CACertPath, "")
	if err != nil {
		return
	}

	parts := strings.Split(box.params.CACredentials, ":")
	box.caClientAuthentication = authentication.NewGRPCBasic(parts[0], parts[1])

	return
}

func (box *Box) initRegistry() (err error) {
	var registryHost string
	if box.params.RegistryAddress == "" {
		registryHost = box.Host()
		box.params.RegistryAddress = fmt.Sprintf("%s%s", box.Host(), RegistryDefaultHost)

	} else {
		parts := strings.Split(box.params.RegistryAddress, ":")
		if len(parts) != 2 {
			return errors.New("malformed registry address. Should be like HOST:PORT")
		}
		registryHost = parts[0]
	}

	syncedRegistry := NewSyncedRegistryServer()
	err = syncedRegistry.Serve(box.Host()+RegistryDefaultHost, box.serverMutualTLS())
	if err != nil {
		log.Println("An instance of registry might already be running on this machine")
		syncedRegistry = nil
		err = nil
	}

	if syncedRegistry == nil || registryHost != "" && registryHost != RegistryDefaultHost && registryHost != box.Host() {
		var syncedRegistry *SyncedRegistry
		var tc *tls.Config
		tc = box.clientMutualTLS()
		syncedRegistry = NewSyncedRegistryClient(box.params.RegistryAddress, tc)
		box.registry = syncedRegistry
	} else {
		box.registry = syncedRegistry
	}
	return
}

func (box *Box) Stop() {
	_ = box.stopServices()
	_ = box.stopGateways()
	if box.registry != nil {
		if sr, ok := box.registry.(*SyncedRegistry); ok {
			sr.Stop()
		}
	}
}
