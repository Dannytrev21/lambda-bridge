package forwarder

import (
	"container/list"
	"log"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Dannytrev21/lambda-bridge/internal/constants"
)

type clientEntry struct {
	client   *http.Client
	host     string
	listElem *list.Element
}

type ClientManager struct {
	clients    map[string]*clientEntry
	lruList    *list.List
	mu         sync.RWMutex
	config     *ClientConfig
	maxClients int

	// Metrics
	evictions  atomic.Uint64
	cacheHits  atomic.Uint64
	cacheMisses atomic.Uint64
}

type ClientConfig struct {
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	MaxConnsPerHost     int
	IdleConnTimeout     time.Duration
	TLSHandshakeTimeout time.Duration
	DisableCompression  bool
	EnableHTTP2         bool
}

func DefaultClientConfig() *ClientConfig {
	return &ClientConfig{
		MaxIdleConns:        500, // Total connection pool size
		MaxIdleConnsPerHost: constants.MaxIdleConnsPerHost,
		MaxConnsPerHost:     constants.MaxConnectionsPerHost,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: constants.ConnectionWarmTimeout,
		DisableCompression:  true, // Webhooks are already JSON
		EnableHTTP2:         true,
	}
}

func NewClientManager(config *ClientConfig) *ClientManager {
	if config == nil {
		config = DefaultClientConfig()
	}
	return &ClientManager{
		clients:    make(map[string]*clientEntry),
		lruList:    list.New(),
		config:     config,
		maxClients: constants.MaxClientPoolSize,
	}
}

func (m *ClientManager) GetClient(botURL string) *http.Client {
	// Parse URL to get host
	u, err := url.Parse(botURL)
	if err != nil {
		return m.getDefaultClient()
	}
	host := u.Host

	m.mu.RLock()
	if entry, exists := m.clients[host]; exists {
		m.mu.RUnlock()
		m.cacheHits.Add(1)
		// Move to front (most recently used)
		m.mu.Lock()
		if entry.listElem != nil {
			m.lruList.MoveToFront(entry.listElem)
		}
		m.mu.Unlock()
		return entry.client
	}
	m.mu.RUnlock()

	m.cacheMisses.Add(1)

	// Create bot-specific client
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if entry, exists := m.clients[host]; exists {
		if entry.listElem != nil {
			m.lruList.MoveToFront(entry.listElem)
		}
		return entry.client
	}

	// Check if we need to evict
	if len(m.clients) >= m.maxClients {
		m.evictLRU()
	}

	client := m.createOptimizedClient()
	elem := m.lruList.PushFront(host)
	m.clients[host] = &clientEntry{
		client:   client,
		host:     host,
		listElem: elem,
	}
	return client
}

// evictLRU removes the least recently used client from the pool.
// Must be called with m.mu held for writing.
func (m *ClientManager) evictLRU() {
	if m.lruList.Len() == 0 {
		return
	}

	// Get least recently used (back of list)
	elem := m.lruList.Back()
	if elem == nil {
		return
	}

	host := elem.Value.(string)
	m.lruList.Remove(elem)

	if entry, exists := m.clients[host]; exists {
		// Close idle connections for evicted client
		if transport, ok := entry.client.Transport.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
		delete(m.clients, host)
		m.evictions.Add(1)
		log.Printf("CLIENT_POOL: Evicted client for host=%s (pool_size=%d)", host, len(m.clients))
	}
}

func (m *ClientManager) createOptimizedClient() *http.Client {
	transport := &http.Transport{
		MaxIdleConns:           m.config.MaxIdleConns,
		MaxIdleConnsPerHost:    m.config.MaxIdleConnsPerHost,
		MaxConnsPerHost:        m.config.MaxConnsPerHost,
		IdleConnTimeout:        m.config.IdleConnTimeout,
		TLSHandshakeTimeout:    m.config.TLSHandshakeTimeout,
		ExpectContinueTimeout:  1 * time.Second,
		DisableCompression:     m.config.DisableCompression,
		ForceAttemptHTTP2:      m.config.EnableHTTP2,
		MaxResponseHeaderBytes: 1 << 20, // 1MB to prevent memory issues

		// Optimized dialer
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
	}

	// Configure HTTP/2 if enabled
	if m.config.EnableHTTP2 {
		transport.ForceAttemptHTTP2 = true
	}

	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
	}
}

func (m *ClientManager) getDefaultClient() *http.Client {
	m.mu.RLock()
	if entry, exists := m.clients["_default"]; exists {
		m.mu.RUnlock()
		return entry.client
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	if entry, exists := m.clients["_default"]; exists {
		return entry.client
	}

	client := m.createOptimizedClient()
	elem := m.lruList.PushFront("_default")
	m.clients["_default"] = &clientEntry{
		client:   client,
		host:     "_default",
		listElem: elem,
	}
	return client
}

func (m *ClientManager) CloseIdleConnections() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, entry := range m.clients {
		if transport, ok := entry.client.Transport.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
	}
}

func (m *ClientManager) GetPoolStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cacheHits := m.cacheHits.Load()
	cacheMisses := m.cacheMisses.Load()
	totalRequests := cacheHits + cacheMisses
	var hitRate float64
	if totalRequests > 0 {
		hitRate = float64(cacheHits) / float64(totalRequests) * 100
	}

	stats := make(map[string]interface{})
	stats["total_clients"] = len(m.clients)
	stats["max_clients"] = m.maxClients
	stats["evictions"] = m.evictions.Load()
	stats["cache_hits"] = cacheHits
	stats["cache_misses"] = cacheMisses
	stats["cache_hit_rate"] = hitRate

	perHost := make(map[string]map[string]int)
	for host, entry := range m.clients {
		if transport, ok := entry.client.Transport.(*http.Transport); ok {
			perHost[host] = map[string]int{
				"max_idle_conns":          m.config.MaxIdleConns,
				"max_idle_conns_per_host": m.config.MaxIdleConnsPerHost,
				"max_conns_per_host":      m.config.MaxConnsPerHost,
			}
			// Note: Go doesn't expose current connection count directly
			_ = transport
		}
	}
	stats["per_host"] = perHost
	return stats
}

// GetClientForHost returns a client for a specific host (exposed method for explicit access).
func (m *ClientManager) GetClientForHost(host string) *http.Client {
	m.mu.RLock()
	if entry, exists := m.clients[host]; exists {
		m.mu.RUnlock()
		m.cacheHits.Add(1)
		m.mu.Lock()
		if entry.listElem != nil {
			m.lruList.MoveToFront(entry.listElem)
		}
		m.mu.Unlock()
		return entry.client
	}
	m.mu.RUnlock()

	m.cacheMisses.Add(1)

	// Create client for host
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if entry, exists := m.clients[host]; exists {
		if entry.listElem != nil {
			m.lruList.MoveToFront(entry.listElem)
		}
		return entry.client
	}

	// Check if we need to evict
	if len(m.clients) >= m.maxClients {
		m.evictLRU()
	}

	client := m.createOptimizedClient()
	elem := m.lruList.PushFront(host)
	m.clients[host] = &clientEntry{
		client:   client,
		host:     host,
		listElem: elem,
	}
	return client
}
