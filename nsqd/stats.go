package nsqd

import (
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nsqio/nsq/internal/quantile"
)

type TopicStats struct {
	TopicName    string         `json:"topic_name"`
	Channels     []ChannelStats `json:"channels"`
	Depth        int64          `json:"depth"`
	BackendDepth int64          `json:"backend_depth"`
	MessageCount uint64         `json:"message_count"`
	Paused       bool           `json:"paused"`

	E2eProcessingLatency *quantile.Result `json:"e2e_processing_latency"`
}

func NewTopicStats(t *Topic, channels []ChannelStats) TopicStats {
	return TopicStats{
		TopicName:    t.name,
		Channels:     channels,
		Depth:        t.Depth(),
		BackendDepth: t.backend.Depth(),
		MessageCount: atomic.LoadUint64(&t.messageCount),
		Paused:       t.IsPaused(),

		E2eProcessingLatency: t.AggregateChannelE2eProcessingLatency().Result(),
	}
}

type ChannelStats struct {
	ChannelName   string        `json:"channel_name"`
	Depth         int64         `json:"depth"`
	BackendDepth  int64         `json:"backend_depth"`
	InFlightCount uint64        `json:"in_flight_count"`
	DeferredCount uint64        `json:"deferred_count"`
	MessageCount  uint64        `json:"message_count"`
	RequeueCount  uint64        `json:"requeue_count"`
	TimeoutCount  uint64        `json:"timeout_count"`
	Clients       []ClientStats `json:"clients"`
	Paused        bool          `json:"paused"`

	E2eProcessingLatency *quantile.Result `json:"e2e_processing_latency"`
}

func NewChannelStats(c *Channel, clients []ClientStats) ChannelStats {
	return ChannelStats{
		ChannelName:   c.name,
		Depth:         c.Depth(),
		BackendDepth:  c.backend.Depth(),
		InFlightCount: atomic.LoadUint64(&c.inFlightCount),
		DeferredCount: atomic.LoadUint64(&c.deferredCount),
		MessageCount:  atomic.LoadUint64(&c.messageCount),
		RequeueCount:  atomic.LoadUint64(&c.requeueCount),
		TimeoutCount:  atomic.LoadUint64(&c.timeoutCount),
		Clients:       clients,
		Paused:        c.IsPaused(),

		E2eProcessingLatency: c.e2eProcessingLatencyStream.Result(),
	}
}

type ClientStats struct {
	ClientID        string `json:"client_id"`
	Hostname        string `json:"hostname"`
	Version         string `json:"version"`
	RemoteAddress   string `json:"remote_address"`
	State           int32  `json:"state"`
	ReadyCount      int64  `json:"ready_count"`
	InFlightCount   int64  `json:"in_flight_count"`
	MessageCount    uint64 `json:"message_count"`
	FinishCount     uint64 `json:"finish_count"`
	RequeueCount    uint64 `json:"requeue_count"`
	ConnectTime     int64  `json:"connect_ts"`
	SampleRate      int32  `json:"sample_rate"`
	Deflate         bool   `json:"deflate"`
	Snappy          bool   `json:"snappy"`
	UserAgent       string `json:"user_agent"`
	Authed          bool   `json:"authed,omitempty"`
	AuthIdentity    string `json:"auth_identity,omitempty"`
	AuthIdentityURL string `json:"auth_identity_url,omitempty"`

	TLS                           bool   `json:"tls"`
	CipherSuite                   string `json:"tls_cipher_suite"`
	TLSVersion                    string `json:"tls_version"`
	TLSNegotiatedProtocol         string `json:"tls_negotiated_protocol"`
	TLSNegotiatedProtocolIsMutual bool   `json:"tls_negotiated_protocol_is_mutual"`
}

type Topics []*Topic

func (t Topics) Len() int      { return len(t) }
func (t Topics) Swap(i, j int) { t[i], t[j] = t[j], t[i] }

type TopicsByName struct {
	Topics
}

func (t TopicsByName) Less(i, j int) bool { return t.Topics[i].name < t.Topics[j].name }

type TopicStatsS []TopicStats

func (t TopicStatsS) Len() int      { return len(t) }
func (t TopicStatsS) Swap(i, j int) { t[i], t[j] = t[j], t[i] }

type TopicStatsByTopicName struct {
	TopicStatsS
}

func (t TopicStatsByTopicName) Less(i, j int) bool {
	return t.TopicStatsS[i].TopicName < t.TopicStatsS[j].TopicName
}

type Channels []*Channel

func (c Channels) Len() int      { return len(c) }
func (c Channels) Swap(i, j int) { c[i], c[j] = c[j], c[i] }

type ChannelsByName struct {
	Channels
}

func (c ChannelsByName) Less(i, j int) bool { return c.Channels[i].name < c.Channels[j].name }

type ChannelStatsS []ChannelStats

func (c ChannelStatsS) Len() int      { return len(c) }
func (c ChannelStatsS) Swap(i, j int) { c[i], c[j] = c[j], c[i] }

type ChannelStatsByChannelName struct {
	ChannelStatsS
}

func (c ChannelStatsByChannelName) Less(i, j int) bool {
	return c.ChannelStatsS[i].ChannelName < c.ChannelStatsS[j].ChannelName
}

func (n *NSQD) GetStats(topic string, channel string) []TopicStats {
	topicAcquireStart := time.Now()
	n.RLock()
	nsqdRlockAcquireDuration := time.Since(topicAcquireStart)
	var realTopics []*Topic
	if topic == "" {
		realTopics = make([]*Topic, 0, len(n.topicMap))
		for _, t := range n.topicMap {
			realTopics = append(realTopics, t)
		}
	} else if val, exists := n.topicMap[topic]; exists {
		realTopics = []*Topic{val}
	} else {
		n.RUnlock()
		return []TopicStats{}
	}
	n.RUnlock()
	topicAcquireDuration := time.Since(topicAcquireStart)
	n.logf(LOG_DEBUG, "stats: acquiring topic list - took %v to acquire nsqd lock", nsqdRlockAcquireDuration)
	n.logf(LOG_DEBUG, "stats: acquired topic list (under lock) in %v", topicAcquireDuration)
	topics := make([]TopicStats, 0, len(realTopics))
	var topicsMutex sync.Mutex
	var topicsWG sync.WaitGroup
	topicsWG.Add(len(realTopics))
	for _, t := range realTopics {
		go func(t *Topic) {
			defer topicsWG.Done()
			topicLockStart := time.Now()
			t.RLock()
			topicLockAcquireDuration := time.Since(topicLockStart)
			var realChannels []*Channel
			if channel == "" {
				realChannels = make([]*Channel, 0, len(t.channelMap))
				for _, c := range t.channelMap {
					realChannels = append(realChannels, c)
				}
			} else if val, exists := t.channelMap[channel]; exists {
				realChannels = []*Channel{val}
			} else {
				t.RUnlock()
				return
			}
			t.RUnlock()
			channelAcquireDuration := time.Since(topicLockStart)
			n.logf(LOG_DEBUG, "stats: topic (%v) rlock acquired in %v", t.name, topicLockAcquireDuration)
			n.logf(LOG_DEBUG, "stats: acquired channels (under lock) for topic (%s) in %v", t.name, channelAcquireDuration)
			channels := make([]ChannelStats, 0, len(realChannels))
			var channelsMutex sync.Mutex
			var channelsWG sync.WaitGroup
			channelsWG.Add(len(realChannels))
			for _, c := range realChannels {
				go func(c *Channel) {
					defer channelsWG.Done()
					channelLockStart := time.Now()
					c.RLock()
					clients := make([]ClientStats, 0, len(c.clients))
					for _, client := range c.clients {
						clients = append(clients, client.Stats())
					}
					c.RUnlock()
					n.logf(LOG_DEBUG, "stats: acquired clients (under lock) for topic/channel (%s/%s) in %v", t.name, c.name, time.Since(channelLockStart))
					channelStatsStart := time.Now()
					// calculate outside lock, as this aggregates e2e latency
					cs := NewChannelStats(c, clients)
					channelsMutex.Lock()
					channels = append(channels, cs)
					channelsMutex.Unlock()
					n.logf(LOG_DEBUG, "stats: acquired channel stats for topic/channel (%s/%s) in %v", t.name, c.name, time.Since(channelStatsStart))
				}(c)
			}
			channelsWG.Wait()
			sort.Sort(ChannelStatsByChannelName{channels})

			topicStatsStart := time.Now()
			// calculate outside lock, as this aggregates e2e latency
			ts := NewTopicStats(t, channels)
			topicsMutex.Lock()
			topics = append(topics, ts)
			topicsMutex.Unlock()
			n.logf(LOG_DEBUG, "stats: acquired topic stats for topic (%s) in %v", t.name, time.Since(topicStatsStart))
		}(t)
	}
	topicsWG.Wait()
	sort.Sort(TopicStatsByTopicName{topics})

	n.logf(LOG_DEBUG, "stats: finished acquiring stats in %v", time.Since(topicAcquireStart))
	return topics
}

type memStats struct {
	HeapObjects       uint64 `json:"heap_objects"`
	HeapIdleBytes     uint64 `json:"heap_idle_bytes"`
	HeapInUseBytes    uint64 `json:"heap_in_use_bytes"`
	HeapReleasedBytes uint64 `json:"heap_released_bytes"`
	GCPauseUsec100    uint64 `json:"gc_pause_usec_100"`
	GCPauseUsec99     uint64 `json:"gc_pause_usec_99"`
	GCPauseUsec95     uint64 `json:"gc_pause_usec_95"`
	NextGCBytes       uint64 `json:"next_gc_bytes"`
	GCTotalRuns       uint32 `json:"gc_total_runs"`
}

func getMemStats() memStats {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	// sort the GC pause array
	length := len(ms.PauseNs)
	if int(ms.NumGC) < length {
		length = int(ms.NumGC)
	}
	gcPauses := make(Uint64Slice, length)
	copy(gcPauses, ms.PauseNs[:length])
	sort.Sort(gcPauses)

	return memStats{
		ms.HeapObjects,
		ms.HeapIdle,
		ms.HeapInuse,
		ms.HeapReleased,
		percentile(100.0, gcPauses, len(gcPauses)) / 1000,
		percentile(99.0, gcPauses, len(gcPauses)) / 1000,
		percentile(95.0, gcPauses, len(gcPauses)) / 1000,
		ms.NextGC,
		ms.NumGC,
	}

}
