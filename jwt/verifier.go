package jwt

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/x509"
	"fmt"
	"github.com/zoenion/common"
	crypto2 "github.com/zoenion/common/crypto"
	"github.com/zoenion/common/errors"
	authpb "github.com/zoenion/common/proto/auth"
	"github.com/zoenion/service/discovery"
	"sync"
)

type RevokedHandlerFunc func()

type jwtVerifier struct {
	sync.Mutex
	registry       discovery.Registry
	storesMutex    sync.Mutex
	tokenVerifiers map[string]authpb.TokenVerifier
	syncedStores   map[string]*SyncedStore
	CaCert         *x509.Certificate
	serviceCert    *x509.Certificate
	serviceKey     crypto.PrivateKey
	cacheDir       string
}

func (j *jwtVerifier) Verify(ctx context.Context, t *authpb.JWT) (authpb.JWTState, error) {
	issuer := t.Claims.Iss

	verifier := j.getJwtVerifier(issuer)
	if verifier == nil {
		s, err := j.registry.GetService(issuer)
		if err != nil {
			return 0, errors.Forbidden
		}

		encodedKey := s.Meta[common.MetaTokenVerifyingKey]
		key, _, err := crypto2.PEMDecodePublicKey([]byte(encodedKey))
		if err != nil {
			return 0, err
		}

		verifier = authpb.NewTokenVerifier(key.(*ecdsa.PublicKey))
		j.saveJwtVerifier(t.Claims.Iss, verifier)
	}

	state, err := verifier.Verify(ctx, t)
	if err != nil {
		return 0, fmt.Errorf("failed to verify to token: %s", errors.Internal)
	}
	if state != authpb.JWTState_VALID {
		return 0, errors.Forbidden
	}

	/*if t.Claims.Store != "" {
		jwtStore := j.getStore(t.Claims.Store)
		if jwtStore == nil {
			ci, err := j.registry.ConnectionInfo(t.Claims.Store, proto.Protocol_Grpc)
			if err != nil {
				return 0, errors.Forbidden
			}

			dictStore, err := persist.NewDBDict(database.SQLiteConfig(filepath.Join(j.cacheDir, "jwt-store.db")), "jwt")
			if err != nil {
				return 0, errors.Internal
			}

			CAPool := x509.NewCertPool()
			CAPool.AddCert(j.CaCert)
			tlsConfig := &tls.Config{
				RootCAs: CAPool,
				Certificates: []tls.Certificate{{
					Certificate: [][]byte{j.serviceCert.Raw},
					PrivateKey:  j.serviceKey,
				}},
			}
			jwtStore = NewSyncedStore(ci.Address, tlsConfig, dictStore)
			j.saveStore(t.Claims.Store, jwtStore)
		}

		state, err = jwtStore.State(t.Claims.Jti)
		if err != nil {
			return state, err
		}
	}*/

	ctx = context.WithValue(ctx, "User", t.Claims.Sub)
	return authpb.JWTState_VALID, nil
}

func (j *jwtVerifier) VerifyJWT(ctx context.Context, jwt string) (authpb.JWTState, error) {
	t, err := authpb.TokenFromJWT(jwt)
	if err != nil {
		return authpb.JWTState_NOT_VALID, err
	}

	return j.Verify(ctx, t)
}

func (j *jwtVerifier) saveJwtVerifier(name string, v authpb.TokenVerifier) {
	j.Lock()
	defer j.Unlock()
	j.tokenVerifiers[name] = v
}

func (j *jwtVerifier) getJwtVerifier(name string) authpb.TokenVerifier {
	j.Lock()
	defer j.Unlock()
	return j.tokenVerifiers[name]
}

func (j *jwtVerifier) getStore(name string) *SyncedStore {
	j.Lock()
	defer j.Unlock()
	return j.syncedStores[name]
}

func (j *jwtVerifier) saveStore(name string, s *SyncedStore) {
	j.Lock()
	defer j.Unlock()
	j.syncedStores[name] = s
}

func NewVerifier(caCert, cert *x509.Certificate, privateKey crypto.PrivateKey, registry discovery.Registry, cacheDir string) authpb.TokenVerifier {
	verifier := &jwtVerifier{
		tokenVerifiers: map[string]authpb.TokenVerifier{},
		syncedStores:   map[string]*SyncedStore{},
		registry:       registry,
		cacheDir:       cacheDir,
		serviceKey:     privateKey,
		serviceCert:    cert,
		CaCert:         caCert,
	}
	return verifier
}
