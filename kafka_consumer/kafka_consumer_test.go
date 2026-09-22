package kafka_consumer

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IBM/sarama"
)

func TestApplyKafkaConsumerOptions(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		opts, err := applyKafkaConsumerOptions()
		if err != nil {
			t.Fatalf("applyKafkaConsumerOptions() error = %v", err)
		}
		if opts.maxMessageAge != 0 {
			t.Fatalf("maxMessageAge = %s, want disabled", opts.maxMessageAge)
		}
	})

	t.Run("maximum message age", func(t *testing.T) {
		opts, err := applyKafkaConsumerOptions(WithMaxMessageAge(5 * time.Minute))
		if err != nil {
			t.Fatalf("applyKafkaConsumerOptions() error = %v", err)
		}
		if opts.maxMessageAge != 5*time.Minute {
			t.Fatalf("maxMessageAge = %s, want 5m", opts.maxMessageAge)
		}
	})

	for _, age := range []time.Duration{0, -time.Second} {
		t.Run("reject "+age.String(), func(t *testing.T) {
			_, err := applyKafkaConsumerOptions(WithMaxMessageAge(age))
			if err == nil || !strings.Contains(err.Error(), "must be positive") {
				t.Fatalf("applyKafkaConsumerOptions() error = %v, want positive-age error", err)
			}
		})
	}

	t.Run("nil option", func(t *testing.T) {
		_, err := applyKafkaConsumerOptions(nil)
		if err == nil || !strings.Contains(err.Error(), "option is nil") {
			t.Fatalf("applyKafkaConsumerOptions() error = %v, want nil-option error", err)
		}
	})
}

func TestIsMessageExpired(t *testing.T) {
	now := time.Date(2026, time.September, 22, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		timestamp   time.Time
		maxAge      time.Duration
		wantExpired bool
	}{
		{name: "older than maximum age", timestamp: now.Add(-5*time.Minute - time.Nanosecond), maxAge: 5 * time.Minute, wantExpired: true},
		{name: "exactly at cutoff", timestamp: now.Add(-5 * time.Minute), maxAge: 5 * time.Minute},
		{name: "newer than cutoff", timestamp: now.Add(-time.Minute), maxAge: 5 * time.Minute},
		{name: "future timestamp", timestamp: now.Add(time.Minute), maxAge: 5 * time.Minute},
		{name: "zero timestamp", maxAge: 5 * time.Minute},
		{name: "filter disabled", timestamp: now.Add(-time.Hour)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isMessageExpired(tt.timestamp, tt.maxAge, now); got != tt.wantExpired {
				t.Fatalf("isMessageExpired() = %t, want %t", got, tt.wantExpired)
			}
		})
	}
}

func TestConsumeClaimSkipsExpiredMessage(t *testing.T) {
	now := time.Now()
	msg := &sarama.ConsumerMessage{
		Topic:     "routes",
		Partition: 2,
		Offset:    41,
		Timestamp: now.Add(-10 * time.Minute),
		Value:     []byte("expired"),
	}
	messages := make(chan *sarama.ConsumerMessage, 1)
	messages <- msg
	close(messages)

	consumerCtx, cancelConsumer := context.WithCancel(context.Background())
	defer cancelConsumer()
	batchCh := make(chan []Message, 1)
	handler := &consumerGroupHandler{consumer: &consumer{
		ctx:           consumerCtx,
		maxMessageAge: 5 * time.Minute,
		topics: []TopicDescr{{
			Name:         "routes",
			BatchChannel: batchCh,
		}},
	}}
	session := newFakeConsumerGroupSession(context.Background())
	claim := &fakeConsumerGroupClaim{topic: "routes", partition: 2, messages: messages}

	if err := handler.ConsumeClaim(session, claim); err != nil {
		t.Fatalf("ConsumeClaim() error = %v", err)
	}

	marked := session.markedMessages()
	if len(marked) != 1 {
		t.Fatalf("marked messages = %d, want 1", len(marked))
	}
	if marked[0].message != msg || marked[0].metadata != "expired" {
		t.Fatalf("marked message = %+v, want expired offset %d", marked[0], msg.Offset)
	}
	select {
	case batch := <-batchCh:
		t.Fatalf("expired message reached batch queue: %+v", batch)
	default:
	}
}

func TestConsumeClaimProcessesNonExpiredMessages(t *testing.T) {
	tests := []struct {
		name      string
		timestamp time.Time
	}{
		{name: "fresh timestamp", timestamp: time.Now()},
		{name: "zero timestamp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessionCtx, cancelSession := context.WithCancel(context.Background())
			defer cancelSession()
			consumerCtx, cancelConsumer := context.WithCancel(context.Background())
			defer cancelConsumer()

			batchCh := make(chan []Message, 1)
			handler := &consumerGroupHandler{consumer: &consumer{
				ctx:           consumerCtx,
				maxMessageAge: 5 * time.Minute,
				topics: []TopicDescr{{
					Name:         "routes",
					BatchChannel: batchCh,
				}},
			}}
			session := newFakeConsumerGroupSession(sessionCtx)
			messages := make(chan *sarama.ConsumerMessage, 1)
			msg := &sarama.ConsumerMessage{
				Topic:     "routes",
				Partition: 1,
				Offset:    17,
				Timestamp: tt.timestamp,
				Value:     []byte("current"),
			}
			messages <- msg
			claim := &fakeConsumerGroupClaim{topic: "routes", partition: 1, messages: messages}
			done := make(chan error, 1)
			go func() {
				done <- handler.ConsumeClaim(session, claim)
			}()

			var batch []Message
			select {
			case batch = <-batchCh:
			case <-time.After(time.Second):
				t.Fatal("message did not reach batch queue")
			}
			if len(batch) != 1 || batch[0].Msg != msg {
				t.Fatalf("batch = %+v, want message offset %d", batch, msg.Offset)
			}
			batch[0].AckCh <- nil

			waitForMarkedMessages(t, session, 1)
			marked := session.markedMessages()
			if marked[0].message != msg || marked[0].metadata != "" {
				t.Fatalf("marked message = %+v, want processed offset %d", marked[0], msg.Offset)
			}

			cancelSession()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("ConsumeClaim() error = %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("ConsumeClaim() did not stop after session cancellation")
			}
		})
	}
}

type markedMessage struct {
	message  *sarama.ConsumerMessage
	metadata string
}

type fakeConsumerGroupSession struct {
	ctx    context.Context
	mu     sync.Mutex
	marked []markedMessage
}

func newFakeConsumerGroupSession(ctx context.Context) *fakeConsumerGroupSession {
	return &fakeConsumerGroupSession{ctx: ctx}
}

func (s *fakeConsumerGroupSession) Claims() map[string][]int32               { return nil }
func (s *fakeConsumerGroupSession) MemberID() string                         { return "test-member" }
func (s *fakeConsumerGroupSession) GenerationID() int32                      { return 1 }
func (s *fakeConsumerGroupSession) MarkOffset(string, int32, int64, string)  {}
func (s *fakeConsumerGroupSession) Commit()                                  {}
func (s *fakeConsumerGroupSession) ResetOffset(string, int32, int64, string) {}
func (s *fakeConsumerGroupSession) Context() context.Context                 { return s.ctx }

func (s *fakeConsumerGroupSession) MarkMessage(msg *sarama.ConsumerMessage, metadata string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.marked = append(s.marked, markedMessage{message: msg, metadata: metadata})
}

func (s *fakeConsumerGroupSession) markedMessages() []markedMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]markedMessage(nil), s.marked...)
}

type fakeConsumerGroupClaim struct {
	topic     string
	partition int32
	messages  <-chan *sarama.ConsumerMessage
}

func (c *fakeConsumerGroupClaim) Topic() string                            { return c.topic }
func (c *fakeConsumerGroupClaim) Partition() int32                         { return c.partition }
func (c *fakeConsumerGroupClaim) InitialOffset() int64                     { return 0 }
func (c *fakeConsumerGroupClaim) HighWaterMarkOffset() int64               { return 0 }
func (c *fakeConsumerGroupClaim) Messages() <-chan *sarama.ConsumerMessage { return c.messages }

func waitForMarkedMessages(t *testing.T, session *fakeConsumerGroupSession, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(session.markedMessages()) >= count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("marked messages = %d, want at least %d", len(session.markedMessages()), count)
}
