package message

import (
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/micro/go-micro/broker"
	"github.com/micro/go-sync/data"
	"github.com/micro/go-sync/lock"
	proto "github.com/microhq/message-srv/proto/message"
)

type memory struct {
	kv data.Data
	br broker.Broker
	lk lock.Lock
}

// namespace:channel

type stream struct {
	Ns    string
	Ch    string
	Clock int64

	// key: id
	Events map[string]*proto.Event
}

var (
	Default *memory

	ErrAlreadyExists = errors.New("already exists")
	ErrNotFound      = errors.New("not found")
)

func newName(n string) string {
	h := sha1.New()
	io.WriteString(h, n)
	return fmt.Sprintf("%x", h.Sum(nil))
}

func newMemory(br broker.Broker, k data.Data, lk lock.Lock) *memory {
	return &memory{
		kv: k,
		br: br,
		lk: lk,
	}
}

func key(arg ...string) string {
	return strings.Join(arg, "-")
}

func Init(br broker.Broker, k data.Data, lk lock.Lock) {
	Default = newMemory(br, k, lk)
	br.Connect()
}

func Create(e *proto.Event) error {
	return Default.Create(e)
}

func Update(e *proto.Event) error {
	return Default.Update(e)
}

func Delete(id, ns, ch string) error {
	return Default.Delete(id, ns, ch)
}

func Read(id, ns, ch string) (*proto.Event, error) {
	return Default.Read(id, ns, ch)
}

func Search(q, ns, ch string, limit, offset int, reverse bool) ([]*proto.Event, error) {
	return Default.Search(q, ns, ch, limit, offset, reverse)
}

func Stream(ns, ch string) (chan *proto.Event, chan bool, error) {
	return Default.Stream(ns, ch)
}

func (s *stream) key() string {
	return s.Ns + s.Ch
}

func (m *memory) Create(e *proto.Event) error {
	if err := m.lk.Acquire(e.Namespace + e.Channel); err != nil {
		return err
	}
	defer m.lk.Release(e.Namespace + e.Channel)

	// get the existing stream
	item, err := m.kv.Read(e.Namespace + e.Channel)
	if err != nil && err != data.ErrNotFound {
		return err
	}

	var st *stream

	// if not found create a new one
	if err == data.ErrNotFound || item == nil || len(item.Value) == 0 {
		st = &stream{
			Ns:     e.Namespace,
			Ch:     e.Channel,
			Clock:  time.Now().Unix(),
			Events: make(map[string]*proto.Event),
		}
	} else {
		if err := json.Unmarshal(item.Value, &st); err != nil {
			return err
		}

		if _, ok := st.Events[e.Id]; ok {
			return ErrAlreadyExists
		}
	}

	st.Events[e.Id] = e

	// marshal the stream
	v, err := json.Marshal(st)
	if err != nil {
		return err
	}

	// put back the stream
	if err := m.kv.Write(&data.Record{
		Key:   st.key(),
		Value: v,
	}); err != nil {
		return err
	}

	// marshal event
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}

	// publish the event
	topic := newName("go.micro.srv.message" + e.Namespace + e.Channel)

	return m.br.Publish(topic, &broker.Message{
		Body: b,
	})
}

func (m *memory) Update(e *proto.Event) error {
	if err := m.lk.Acquire(e.Namespace + e.Channel); err != nil {
		return err
	}
	defer m.lk.Release(e.Namespace + e.Channel)

	// get the existing stream
	item, err := m.kv.Read(e.Namespace + e.Channel)
	if err != nil && err != data.ErrNotFound {
		return err
	}

	var st *stream

	// if not found create a new one
	if err == data.ErrNotFound || item == nil || len(item.Value) == 0 {
		st = &stream{
			Ns:     e.Namespace,
			Ch:     e.Channel,
			Clock:  time.Now().Unix(),
			Events: make(map[string]*proto.Event),
		}
	} else {
		if err := json.Unmarshal(item.Value, &st); err != nil {
			return err
		}
	}

	st.Events[e.Id] = e

	// marshal the stream
	v, err := json.Marshal(st)
	if err != nil {
		return err
	}

	// set the value
	item.Value = v

	// put back the stream
	if err := m.kv.Write(&data.Record{
		Key:   st.key(),
		Value: v,
	}); err != nil {
		return err
	}

	// marshal event
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}

	// publish the event
	topic := newName("go.micro.srv.message" + e.Namespace + e.Channel)
	return m.br.Publish(topic, &broker.Message{
		Body: b,
	})
}

func (m *memory) Delete(id, ns, ch string) error {
	if err := m.lk.Acquire(ns + ch); err != nil {
		return err
	}
	defer m.lk.Release(ns + ch)

	// get the existing stream
	item, err := m.kv.Read(ns + ch)
	if err != nil && err != data.ErrNotFound {
		return err
	}

	if err == data.ErrNotFound || item == nil {
		return nil
	}

	var st *stream

	if err := json.Unmarshal(item.Value, &st); err != nil {
		return err
	}

	delete(st.Events, id)

	// marshal the stream
	v, err := json.Marshal(st)
	if err != nil {
		return err
	}

	// set the value
	item.Value = v

	// put back the stream
	return m.kv.Write(item)
}

func (m *memory) Read(id, ns, ch string) (*proto.Event, error) {
	if err := m.lk.Acquire(ns + ch); err != nil {
		return nil, err
	}
	defer m.lk.Release(ns + ch)

	// get the existing stream
	item, err := m.kv.Read(ns + ch)
	if err != nil && err != data.ErrNotFound {
		return nil, err
	}

	if err == data.ErrNotFound || item == nil || len(item.Value) == 0 {
		return nil, ErrNotFound
	}

	var st *stream

	if err := json.Unmarshal(item.Value, &st); err != nil {
		return nil, err
	}

	e, ok := st.Events[id]
	if !ok {
		return nil, ErrNotFound
	}

	return e, nil
}

func (m *memory) Search(q, ns, ch string, limit, offset int, reverse bool) ([]*proto.Event, error) {
	if err := m.lk.Acquire(ns + ch); err != nil {
		return nil, err
	}
	defer m.lk.Release(ns + ch)

	// get the existing stream
	item, err := m.kv.Read(ns + ch)
	if err != nil && err != data.ErrNotFound {
		return nil, err
	}

	if err == data.ErrNotFound || item == nil || len(item.Value) == 0 {
		return nil, ErrNotFound
	}

	var st *stream

	if err := json.Unmarshal(item.Value, &st); err != nil {
		return nil, err
	}

	if i := len(st.Events); i == 0 || offset >= i {
		return []*proto.Event{}, nil
	}

	// TODO: use query
	var events []*proto.Event
	for _, event := range st.Events {
		events = append(events, event)
	}

	sort.Sort(sortedEvents{events})

	var evs []*proto.Event

	if reverse {
		// flip the offset
		offset = len(events) - offset - 1
	}

	for i := 0; i < limit; i++ {
		// make sure we don't cross the boundaries
		if offset < 0 || offset >= len(events) {
			break
		}

		evs = append(evs, events[offset])

		if reverse {
			offset--
		} else {
			offset++
		}
	}

	return evs, nil
}

func (m *memory) Stream(ns, ch string) (chan *proto.Event, chan bool, error) {
	che := make(chan *proto.Event, 100)
	exit := make(chan bool)

	topic := newName("go.micro.srv.message" + ns + ch)
	sub, err := m.br.Subscribe(topic, func(p broker.Publication) error {
		var e *proto.Event
		if err := json.Unmarshal(p.Message().Body, &e); err != nil {
			return err
		}
		che <- e
		return nil
	})

	if err != nil {
		return nil, nil, err
	}

	go func() {
		<-exit
		sub.Unsubscribe()
	}()

	return che, exit, nil
}
