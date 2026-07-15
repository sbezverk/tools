package kafka_consumer

import (
	"context"
	"math/rand"
	"strconv"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/golang/glog"
)

type ObservedKafkaMessage struct {
	Timestamp time.Time
	Topic     string
	Partition int32
	Offset    int64
	Body      map[string]any
	Raw       []byte
}

type KafkaConsumerConfig struct {
	Brokers        []string `yaml:"brokers"`
	ConsumerGroups []string `yaml:"consumer-groups"`
	Topics         []string `yaml:"topics"`
}

type TopicDescr struct {
	Name         string
	BatchChannel chan []Message
}

// KafkaConsumer is an improved Kafka consumer with predictable memory usage
type KafkaConsumer interface {
	Start()
	Stop()
	GetTopics() []TopicDescr
}

type consumer struct {
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	consumerGroup sarama.ConsumerGroup
	topics        []TopicDescr
	config        *sarama.Config
	brokers       []string
	groupID       string
	mtx           sync.Mutex
	ready         chan struct{} // Signals when consumer group is ready
}

func (c *consumer) GetTopics() []TopicDescr {
	return c.topics
}

// Retry constants for exponential backoff when connecting to the Kafka broker.
const (
	retryInitialInterval = 2 * time.Second
	retryMaxInterval     = 60 * time.Second
	retryMultiplier      = 2.0

	workersPerTopic = 1

	// Work channel buffer per topic - large buffer for burst absorption
	// Acts as elastic queue between fast Kafka and slower DB writes
	// At ~500 bytes/msg: 10,000 msgs = ~5MB per topic
	workChannelBuffer = 10000

	// batch size - sweet spot for bulk writes (500-1000 documents)
	// Balances throughput (larger is faster) vs latency (smaller is more responsive)
	batchSize = 500

	// batch timeout - flush partial batches to maintain low latency
	// Even during slow message flow, don't wait more than 100ms
	batchTimeout = 100 * time.Millisecond

	// Retry interval when partition is not ready
	partitionRetryInterval = 200 * time.Millisecond

	// Graceful shutdown timeout
	shutdownTimeout = 30 * time.Second

	// Maximum time Start waits for Sarama to call Setup for the first session.
	startupReadyTimeout = 30 * time.Second
)

// NewKafkaConsumer creates an improved Kafka consumer with bounded memory usage.
// If the broker is unreachable it retries with exponential backoff (up to 60 s) instead
// of returning an error immediately, keeping the application alive.
// The retry loop is aborted when ctx is cancelled (e.g. on SIGINT during startup).
func NewKafkaConsumer(ctx context.Context, name string, groupID string, cfg *KafkaConsumerConfig) (KafkaConsumer, error) {
	config := sarama.NewConfig()
	// Generate unique client ID (auto-seeded rand in Go 1.20+)
	config.ClientID = groupID + "_" + strconv.Itoa(rand.Intn(100000))
	config.Consumer.Return.Errors = true
	config.Version = sarama.V3_0_0_0

	// Network configuration for Kubernetes NodePort / NAT environments
	// KeepAlive prevents idle connection timeouts (common with K8s NodePort ~10min timeout)
	// Short timeouts help detect connection failures faster and reconnect
	config.Net.DialTimeout = 10 * time.Second
	config.Net.ReadTimeout = 10 * time.Second
	config.Net.WriteTimeout = 10 * time.Second
	config.Net.KeepAlive = 30 * time.Second // Prevent idle connection timeouts

	// Metadata refresh to handle broker address changes
	// Important: If Kafka returns internal cluster IPs in metadata, connection will fail
	// Ensure Kafka's advertised.listeners uses externally routable addresses
	config.Metadata.Retry.Max = 3
	config.Metadata.Retry.Backoff = 250 * time.Millisecond
	config.Metadata.RefreshFrequency = 5 * time.Minute // Periodic refresh

	// Consumer-specific settings
	config.Consumer.Fetch.Default = 1024 * 1024         // 1MB default fetch
	config.Consumer.MaxProcessingTime = 1 * time.Minute // Max time to process a batch
	config.ChannelBufferSize = 1000                     // Internal Sarama buffer

	// Consumer group configuration for offset management
	config.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRoundRobin()}
	config.Consumer.Offsets.Initial = sarama.OffsetOldest // Start from beginning for new consumers
	config.Consumer.Offsets.AutoCommit.Enable = true      // Auto-commit offsets
	config.Consumer.Offsets.AutoCommit.Interval = 1 * time.Second

	// Retry loop with exponential backoff: keeps the application alive while the Kafka
	// broker is temporarily unavailable (e.g. rolling restart, startup ordering in Kubernetes).
	var consumerGroup sarama.ConsumerGroup
	delay := retryInitialInterval
	for attempt := 1; ; attempt++ {
		var err error
		consumerGroup, err = sarama.NewConsumerGroup(cfg.Brokers, groupID, config)
		if err == nil {
			break
		}
		glog.Warningf("Kafka consumer group creation attempt %d failed: %v, retrying in %v", attempt, err, delay)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		delay = time.Duration(float64(delay) * retryMultiplier)
		if delay > retryMaxInterval {
			delay = retryMaxInterval
		}
	}
	consumerCtx, cancel := context.WithCancel(ctx)

	c := &consumer{
		ctx:           consumerCtx,
		cancel:        cancel,
		consumerGroup: consumerGroup,
		brokers:       cfg.Brokers,
		groupID:       groupID,
		config:        config,
	}
	c.topics = make([]TopicDescr, len(cfg.Topics))
	for i := 0; i < len(cfg.Topics); i++ {
		c.topics[i] = TopicDescr{
			Name:         cfg.Topics[i],
			BatchChannel: make(chan []Message, workChannelBuffer),
		}
	}

	return c, nil
}

// consumerGroupHandler implements sarama.ConsumerGroupHandler for consumer group processing
type consumerGroupHandler struct {
	consumer *consumer
}

// Setup is run at the beginning of a new session, before ConsumeClaim
func (h *consumerGroupHandler) Setup(sarama.ConsumerGroupSession) error {
	// Mark the consumer as ready
	h.consumer.signalReady()
	return nil
}

// Cleanup is run at the end of a session, once all ConsumeClaim goroutines have exited
func (h *consumerGroupHandler) Cleanup(sarama.ConsumerGroupSession) error {
	return nil
}

type Message struct {
	Msg   *sarama.ConsumerMessage
	AckCh chan error
}

type AckResult struct {
	Msg *sarama.ConsumerMessage
	Err error
}

// ConsumeClaim must start a consumer loop of ConsumerGroupClaim's Messages().
// This replaces the old topicReader + workerBatched pattern with consumer group support
func (h *consumerGroupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	topic := claim.Topic()

	// Find the topic processor for this topic
	var topicCfg TopicDescr
	for _, t := range h.consumer.topics {
		if t.Name == topic {
			topicCfg = t
			break
		}
	}

	glog.Infof("Starting consumer for topic %s, partition %d", topic, claim.Partition())

	// Create work channel with buffer
	workCh := make(chan Message, workChannelBuffer)
	ackResult := make(chan AckResult, workChannelBuffer)
	// Start fixed worker pool for this partition
	workerWg := &sync.WaitGroup{}
	for i := 0; i < workersPerTopic; i++ {
		workerWg.Add(1)
		go h.consumer.workerBatched(i, topicCfg, workCh, workerWg)
	}

	// Defer cleanup
	defer func() {
		close(workCh)
		workerWg.Wait()
		glog.Infof("Consumer for topic %s partition %d stopped", topic, claim.Partition())
	}()

	// Main message consumption loop
	// NOTE: Do not move the code below to a goroutine. The ConsumeClaim method
	// should return when the session ends to allow rebalancing
	for {
		select {
		case ack := <-ackResult:
			if ack.Err != nil {
				glog.Errorf("Worker failed to process message: %v", ack.Err)
				session.MarkMessage(ack.Msg, ack.Err.Error())
			} else {
				session.MarkMessage(ack.Msg, "")
			}
		case msg := <-claim.Messages():
			if msg == nil {
				return nil
			}

			if glog.V(5) {
				glog.Infof("Received message from topic %s: partition=%d offset=%d size=%d",
					topic, msg.Partition, msg.Offset, len(msg.Value))
			}

			// Send message to worker pool
			m := Message{Msg: msg, AckCh: make(chan error, 1)}
			select {
			case workCh <- m:
				// Message sent to worker

				// Mark message as processed (consumer group will commit this offset)
				// This happens automatically when the session commits, but we mark it here

				// For now, wait uncoditionally for ack from the worker before marking the message. This ensures we don't mark messages as processed until they have been handled.
				go func(m Message) {
					select {
					case err := <-m.AckCh:
						ackResult <- AckResult{Msg: m.Msg, Err: err}
					case <-session.Context().Done():
						return
					}
				}(m)

			case <-session.Context().Done():
				return nil
			}

		case <-session.Context().Done():
			return nil
		}
	}
}

func (c *consumer) ensureReadyChannel() chan struct{} {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if c.ready == nil {
		c.ready = make(chan struct{})
	}
	return c.ready
}

func (c *consumer) signalReady() {
	c.mtx.Lock()
	ch := c.ready
	c.ready = nil
	c.mtx.Unlock()

	if ch != nil {
		close(ch)
	}
}

func (c *consumer) Start() {
	glog.Infof("Starting Kafka consumer group %s for %d topics", c.groupID, len(c.topics))

	// Get topic names
	topicNames := make([]string, len(c.topics))
	for i, t := range c.topics {
		topicNames[i] = t.Name
	}

	// Create handler
	handler := &consumerGroupHandler{consumer: c}

	// Start consumer group consumption in a goroutine
	// Consumer groups handle partition assignment and rebalancing automatically
	c.wg.Add(1)
	ready := c.ensureReadyChannel()
	go func() {
		defer c.wg.Done()
		for {
			// Consume will automatically handle rebalancing.
			// It blocks until the session ends (cancel or error).
			if err := c.consumerGroup.Consume(c.ctx, topicNames, handler); err != nil {
				glog.Errorf("Consumer group error: %v", err)
			}

			// Check if context was cancelled, signaling that the consumer should stop.
			if c.ctx.Err() != nil {
				glog.Info("Consumer group stopped")
				return
			}
			c.ensureReadyChannel()
		}
	}()

	// Await till the consumer has been set up
	select {
	case <-ready:
		glog.Infof("Kafka consumer group ready and consuming")
	case <-time.After(startupReadyTimeout):
		glog.Warningf("Kafka consumer group did not become ready within %v; consume loop will continue retrying in the background", startupReadyTimeout)
		return
	case <-c.ctx.Done():
		glog.Infof("Kafka consumer startup canceled before ready")
		return
	}
}

func (c *consumer) Stop() {
	glog.Info("Stopping Kafka consumer group...")

	// Step 1: Cancel context to ask Consume() loop to stop.
	c.cancel()

	// Step 2: Close the consumer group early. In practice this helps unblock
	// Consume() promptly on shutdown instead of waiting for the consume loop to
	// notice context cancellation on its own.
	if err := c.consumerGroup.Close(); err != nil {
		glog.Errorf("Error closing consumer group: %v", err)
	} else {
		glog.Info("Consumer group closed successfully")
	}

	// Step 3: Wait for consumer group loop to finish with timeout.
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		glog.Info("Consumer group loop stopped gracefully")
	case <-time.After(shutdownTimeout):
		glog.Warning("Shutdown timeout exceeded for consumer group loop")
	}
}

// workerBatched accumulates messages into batches for bulk writes
// Flushes when batch is full or timeout expires (ensures low latency)
func (c *consumer) workerBatched(id int, topicCfg TopicDescr, workCh <-chan Message, wg *sync.WaitGroup) {
	defer wg.Done()

	batch := make([]Message, 0, batchSize)
	ticker := time.NewTicker(batchTimeout)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}

		// Create a copy of the batch to send
		batchCopy := make([]Message, len(batch))
		copy(batchCopy, batch)

		// Send batch to topic channel for bulk insert
		select {
		case topicCfg.BatchChannel <- batchCopy:
			// Successfully delivered batch
			if glog.V(5) {
				glog.Infof("Worker %d: flushed batch of %d messages for topic %s", id, len(batch), topicCfg.Name)
			}
			batch = batch[:0] // Reset batch
		case <-c.ctx.Done():
			return
		}
	}

	for {
		select {
		case msg, ok := <-workCh:
			if !ok {
				// Work channel closed, flush remaining and exit
				flush()
				return
			}

			batch = append(batch, msg)
			if glog.V(5) {
				glog.Infof("Worker %d: received message for topic %s, batch size now %d", id, topicCfg.Name, len(batch))
			}
			// Flush when batch reaches target size
			if len(batch) >= batchSize {
				flush()
			}

		case <-ticker.C:
			// Flush partial batch on timeout (ensures low latency even during slow flow)
			flush()

		case <-c.ctx.Done():
			flush()
			return
		}
	}
}
