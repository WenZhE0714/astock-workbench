package notify

import (
	"context"
	"fmt"
	"log"
	"time"
)

const MaxAge = 5 * time.Minute

type Message struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Subtitle string    `json:"subtitle"`
	Body     string    `json:"body"`
	At       time.Time `json:"at"`
}

type Record struct {
	Message
	Status      string    `json:"status"`
	Attempts    int       `json:"attempts"`
	SubmittedAt time.Time `json:"submitted_at,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
}

type State struct {
	Version int               `json:"version"`
	Records map[string]Record `json:"records"`
}

type Store interface {
	Load() (State, error)
	Save(State) error
}

type Sender interface {
	Send(context.Context, Message) error
}

// Dispatcher owns notification state on one worker. Market scanning only
// enqueues messages; a slow or unavailable desktop cannot block trading data.
type Dispatcher struct {
	sender Sender
	store  Store
	state  State
	queue  chan Message
	logger *log.Logger
	now    func() time.Time
}

func NewDispatcher(sender Sender, store Store, logger *log.Logger) (*Dispatcher, error) {
	state, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("读取 Mac 通知记录: %w", err)
	}
	if state.Records == nil {
		state.Records = make(map[string]Record)
	}
	return &Dispatcher{sender: sender, store: store, state: state, queue: make(chan Message, 2048), logger: logger, now: time.Now}, nil
}

func (d *Dispatcher) Offer(message Message) {
	if d == nil || message.ID == "" {
		return
	}
	select {
	case d.queue <- message:
	default:
		d.logger.Printf("Mac 通知队列已满，未接收事件 %s", message.ID)
	}
}

func Fresh(at, now time.Time) bool {
	return !at.IsZero() && !at.After(now.Add(5*time.Second)) && now.Sub(at) <= MaxAge
}

func (d *Dispatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	d.retry(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case message := <-d.queue:
			if ctx.Err() != nil {
				return
			}
			d.deliver(ctx, message)
		case <-ticker.C:
			d.retry(ctx)
		}
	}
}

func (d *Dispatcher) deliver(ctx context.Context, message Message) {
	now := d.now()
	previous, exists := d.state.Records[message.ID]
	if exists && (previous.Status == "submitted" || previous.Status == "expired" || previous.Attempts >= 3) {
		return
	}
	if exists {
		message = previous.Message
	}
	if !Fresh(message.At, now) {
		if exists {
			previous.Status = "expired"
			d.state.Records[message.ID] = previous
			d.save()
		}
		return
	}
	for id, record := range d.state.Records {
		if now.Sub(record.At) > 48*time.Hour {
			delete(d.state.Records, id)
		}
	}
	if !exists {
		previous = Record{Message: message}
	}
	previous.Status = "pending"
	previous.Attempts++
	d.state.Records[message.ID] = previous
	// Persist before sending so interrupted attempts can resume after restart.
	if !d.save() {
		previous.Attempts--
		d.state.Records[message.ID] = previous
		return
	}
	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err := d.sender.Send(sendCtx, previous.Message)
	cancel()
	if err != nil {
		previous.Status, previous.LastError = "failed", err.Error()
		d.logger.Printf("Mac 通知发送失败 %s: %v", message.ID, err)
	} else {
		// Successful submission is not proof that macOS displayed a banner.
		previous.Status, previous.LastError = "submitted", ""
		previous.SubmittedAt = d.now()
	}
	d.state.Records[message.ID] = previous
	d.save()
}

func (d *Dispatcher) retry(ctx context.Context) {
	for _, record := range d.state.Records {
		if ctx.Err() != nil {
			return
		}
		if record.Status == "pending" || record.Status == "failed" {
			d.deliver(ctx, record.Message)
		}
	}
}

func (d *Dispatcher) save() bool {
	if err := d.store.Save(d.state); err != nil {
		d.logger.Printf("保存 Mac 通知记录失败: %v", err)
		return false
	}
	return true
}
