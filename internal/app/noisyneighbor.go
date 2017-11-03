package app

import (
	"crypto/tls"
	"log"

	"code.cloudfoundry.org/noisyneighbor/internal/store"
	"code.cloudfoundry.org/noisyneighbor/internal/web"
	"github.com/cloudfoundry/noaa/consumer"
)

// NoisyNeighbor is the top level data structure for the NoisyNeighbor
// application.
type NoisyNeighbor struct {
	cfg        Config
	server     *web.Server
	ingestor   *Ingestor
	processor  *Processor
	aggregator *store.Aggregator
}

// New returns an initialized NoisyNeighbor. This will authenticate with UAA,
// open a connection to the firehose, and initialize all subprocesses.
// TODO: Authenticate with UAA.
// TODO: store/store calculates rates
func New(cfg Config) *NoisyNeighbor {
	cnsmr := consumer.New(
		cfg.LoggregatorAddr,
		&tls.Config{InsecureSkipVerify: true}, // TODO: This should be configurable
		nil,
	)

	// TODO: Fetch auth token from UAA
	msgs, errs := cnsmr.FilteredFirehose(cfg.SubscriptionID, "", consumer.LogMessages)
	go func() {
		for err := range errs {
			log.Printf("error received from firehose: %s", err)
		}
	}()

	b := NewBuffer(cfg.BufferSize)
	c := store.NewCounter()
	a := store.NewAggregator(c,
		store.WithPollingInterval(cfg.PollingInterval),
	)
	s := web.NewServer(
		cfg.Port,
		cfg.BasicAuthCreds.Username,
		cfg.BasicAuthCreds.Password,
		a.State,
	)

	return &NoisyNeighbor{
		cfg:        cfg,
		server:     s,
		aggregator: a,
		ingestor:   NewIngestor(msgs, b.Set),
		processor:  NewProcessor(b.Next, c.Inc),
	}
}

// Addr returns the address that the NoisyNeighbor is bound to.
func (nn *NoisyNeighbor) Addr() string {
	return nn.server.Addr()
}

// Run starts the NoisyNeighbor application. This is a blocking method call.
func (nn *NoisyNeighbor) Run() {
	go nn.ingestor.Run()
	go nn.processor.Run()
	go nn.aggregator.Run()

	nn.server.Serve()
}

// Stop gracefully stops the NoisyNeighbor application. It will disconnect from
// the firehose, and complete any active HTTP requests.
func (nn *NoisyNeighbor) Stop() {
	nn.server.Stop()
}