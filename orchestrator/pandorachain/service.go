package pandorachain

import (
	"context"
	"fmt"
	"sync"
	"time"

	eth1Types "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/lukso-network/lukso-orchestrator/orchestrator/cache"
	"github.com/lukso-network/lukso-orchestrator/orchestrator/db"
	"github.com/lukso-network/lukso-orchestrator/shared/types"
)

// time to wait before trying to reconnect.
var reConPeriod = 15 * time.Second

// DialRPCFn dials to the given endpoint
type DialRPCFn func(endpoint string) (*rpc.Client, error)

// Service
// 	- maintains connection with pandora chain
//  - maintains db and cache to store the in-coming headers from pandora.
type Service struct {
	// service maintenance related attributes
	isRunning      bool
	processingLock sync.RWMutex
	ctx            context.Context
	cancel         context.CancelFunc
	runError       error

	// pandora chain related attributes
	connected bool
	endpoint  string
	rpcClient *rpc.Client
	dialRPCFn DialRPCFn
	namespace string

	// subscription
	conInfoSubErrCh    chan error
	conInfoSub         *rpc.ClientSubscription
	pendingHeadersChan chan *eth1Types.Header
	pendingWorkChannel chan *types.HeaderHash

	// db support
	db    db.Database
	cache cache.PandoraHeaderCache
}

// NewService creates new service with pandora ws or ipc endpoint, pandora service namespace and db
func NewService(ctx context.Context, endpoint string, namespace string,
	db db.Database, cache cache.PandoraHeaderCache, dialRPCFn DialRPCFn) (*Service, error) {

	ctx, cancel := context.WithCancel(ctx)
	_ = cancel // govet fix for lost cancel. Cancel is handled in service.Stop()
	return &Service{
		ctx:                ctx,
		cancel:             cancel,
		endpoint:           endpoint,
		dialRPCFn:          dialRPCFn,
		namespace:          namespace,
		conInfoSubErrCh:    make(chan error),
		pendingHeadersChan: make(chan *eth1Types.Header, 10000000),
		pendingWorkChannel: make(chan *types.HeaderHash, 10000000),
		db:                 db,
		cache:              cache,
	}, nil
}

// Start a consensus info fetcher service's main event loop.
func (s *Service) Start() {
	// Exit early if pandora endpoint is not set.
	if s.endpoint == "" {
		return
	}
	go func() {
		s.isRunning = true
		s.waitForConnection()
		if s.ctx.Err() != nil {
			log.Info("Context closed, exiting pandora goroutine")
			return
		}
		s.run(s.ctx.Done())
	}()
}

func (s *Service) Stop() error {
	if s.cancel != nil {
		defer s.cancel()
	}
	s.closeClients()
	return nil
}

func (s *Service) Status() error {
	// Service don't start
	if !s.isRunning {
		return nil
	}
	// get error from run function
	if s.runError != nil {
		return s.runError
	}
	return nil
}

func (s *Service) SubscribeToPendingWorkChannel(subscriberChan chan<- *types.HeaderHash) (err error) {
	if nil == s.pendingWorkChannel {
		err = fmt.Errorf("pendingHeadersChan cannot be nil")

		return
	}

	go func() {
		for {
			pendingWork := <-s.pendingWorkChannel
			subscriberChan <- pendingWork
		}
	}()

	return
}

// closes down our active eth1 clients.
func (s *Service) closeClients() {
	if s.rpcClient != nil {
		s.rpcClient.Close()
	}
}

// waitForConnection waits for a connection with pandora chain. Until a successful connection and subscription with
// pandora chain, it retries again and again.
func (s *Service) waitForConnection() {
	log.Debug("Waiting for the connection")
	var err error
	if err = s.connectToChain(); err == nil {
		log.WithField("endpoint", s.endpoint).Info("Connected and subscribed to pandora chain")
		s.connected = true
		return
	}
	log.WithError(err).Warn("Could not connect or subscribe to pandora chain")
	s.runError = err
	ticker := time.NewTicker(reConPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			log.WithField("endpoint", s.endpoint).Debug("Dialing pandora node")
			var errConnect error
			if errConnect = s.connectToChain(); errConnect != nil {
				log.WithError(errConnect).Warn("Could not connect or subscribe to pandora chain")
				s.runError = errConnect
				continue
			}
			s.connected = true
			s.runError = nil
			log.WithField("endpoint", s.endpoint).Info("Connected and subscribed to pandora chain")
			return
		case <-s.ctx.Done():
			log.Debug("Received cancelled context,closing existing pandora client service")
			return
		}
	}
}

// run subscribes to all the services for the ETH1.0 chain.
func (s *Service) run(done <-chan struct{}) {
	log.Debug("Pandora chain service is starting")
	s.runError = nil

	// the loop waits for any error which comes from consensus info subscription
	// if any subscription error happens, it will try to reconnect and re-subscribe with pandora chain again.
	for {
		select {
		case <-done:
			s.isRunning = false
			s.runError = nil
			log.Debug("Context closed, exiting goroutine")
			return
		case err := <-s.conInfoSubErrCh:
			log.WithError(err).Debug("Got subscription error")
			log.Debug("Starting retry to connect and subscribe to pandora chain")
			// Try to check the connection and retry to establish the connection
			s.retryToConnectAndSubscribe(err)
			continue
		}
	}
}

// connectToChain dials to pandora chain and creates rpcClient and subscribe
func (s *Service) connectToChain() error {
	if s.rpcClient == nil {
		panRPCClient, err := s.dialRPCFn(s.endpoint)
		if err != nil {
			return err
		}
		s.rpcClient = panRPCClient
	}

	// connect to pandora subscription
	if err := s.subscribe(); err != nil {
		return err
	}
	return nil
}

// retryToConnectAndSubscribe retries to pandora chain in case of any failure.
func (s *Service) retryToConnectAndSubscribe(err error) {
	s.runError = err
	s.connected = false
	// Back off for a while before resuming dialing the pandora node.
	time.Sleep(reConPeriod)
	s.waitForConnection()
	// Reset run error in the event of a successful connection.
	s.runError = nil
}

// subscribe subscribes to pandora events
func (s *Service) subscribe() error {
	latestSavedHeaderHash := s.db.GetLatestHeaderHash()
	filter := &types.PandoraPendingHeaderFilter{
		FromBlockHash: latestSavedHeaderHash,
	}
	// subscribe to pandora client for pending headers
	sub, err := s.SubscribePendingHeaders(s.ctx, filter, s.namespace, s.rpcClient)
	if err != nil {
		log.WithError(err).Warn("Could not subscribe to pandora client for new pending headers")
		return err
	}
	s.conInfoSub = sub
	return nil
}
