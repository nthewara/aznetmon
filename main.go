package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

type TestResult struct {
	Target    string    `json:"target"`
	Protocol  string    `json:"protocol"`
	Port      int       `json:"port,omitempty"`
	Success   bool      `json:"success"`
	Duration  float64   `json:"duration_ms"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

type Monitor struct {
	icmpTargets []string
	tcpTargets  map[string]int // host -> port mapping
	results     map[string]*TestResult
	mutex       sync.RWMutex
	clients     map[*websocket.Conn]*sync.Mutex // Change to store per-connection mutex
	clientsMux  sync.RWMutex
	broadcast   chan TestResult
	upgrader    websocket.Upgrader
}

func NewMonitor(icmpTargets []string, tcpTargets map[string]int) *Monitor {
	return &Monitor{
		icmpTargets: icmpTargets,
		tcpTargets:  tcpTargets,
		results:     make(map[string]*TestResult),
		clients:     make(map[*websocket.Conn]*sync.Mutex),
		broadcast:   make(chan TestResult),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins in this simple app
			},
		},
	}
}

func (m *Monitor) ping(target string) TestResult {
	start := time.Now()

	// Resolve the address
	dst, err := net.ResolveIPAddr("ip4", target)
	if err != nil {
		return TestResult{
			Target:    target,
			Protocol:  "ICMP",
			Success:   false,
			Duration:  0,
			Error:     fmt.Sprintf("Failed to resolve %s: %v", target, err),
			Timestamp: time.Now(),
		}
	}

	// Create ICMP connection
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return TestResult{
			Target:    target,
			Protocol:  "ICMP",
			Success:   false,
			Duration:  0,
			Error:     fmt.Sprintf("Failed to create ICMP socket: %v", err),
			Timestamp: time.Now(),
		}
	}
	defer conn.Close()

	// Create ICMP message
	message := &icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   os.Getpid() & 0xffff,
			Seq:  1,
			Data: []byte("aznetmon"),
		},
	}

	data, err := message.Marshal(nil)
	if err != nil {
		return TestResult{
			Target:    target,
			Protocol:  "ICMP",
			Success:   false,
			Duration:  0,
			Error:     fmt.Sprintf("Failed to marshal ICMP message: %v", err),
			Timestamp: time.Now(),
		}
	}

	// Set timeout
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))

	// Send packet
	_, err = conn.WriteTo(data, dst)
	if err != nil {
		return TestResult{
			Target:    target,
			Protocol:  "ICMP",
			Success:   false,
			Duration:  0,
			Error:     fmt.Sprintf("Failed to send ICMP packet: %v", err),
			Timestamp: time.Now(),
		}
	}

	// Read reply
	reply := make([]byte, 1500)
	_, _, err = conn.ReadFrom(reply)
	duration := time.Since(start)

	if err != nil {
		return TestResult{
			Target:    target,
			Protocol:  "ICMP",
			Success:   false,
			Duration:  0,
			Error:     fmt.Sprintf("Failed to receive ICMP reply: %v", err),
			Timestamp: time.Now(),
		}
	}

	return TestResult{
		Target:    target,
		Protocol:  "ICMP",
		Success:   true,
		Duration:  float64(duration.Nanoseconds()) / 1e6, // Convert to milliseconds
		Timestamp: time.Now(),
	}
}

func (m *Monitor) tcpTest(target string, port int) TestResult {
	start := time.Now()

	// Create address with port
	address := fmt.Sprintf("%s:%d", target, port)

	// Set a timeout for the connection
	dialer := net.Dialer{
		Timeout: 3 * time.Second,
	}

	// Attempt to establish TCP connection
	conn, err := dialer.Dial("tcp", address)
	if err != nil {
		return TestResult{
			Target:    target,
			Protocol:  "TCP",
			Port:      port,
			Success:   false,
			Duration:  0,
			Error:     fmt.Sprintf("Failed TCP connection to %s: %v", address, err),
			Timestamp: time.Now(),
		}
	}

	// Close the connection
	conn.Close()

	duration := time.Since(start)

	// Return successful result
	return TestResult{
		Target:    target,
		Protocol:  "TCP",
		Port:      port,
		Success:   true,
		Duration:  float64(duration.Nanoseconds()) / 1e6, // Convert to milliseconds
		Timestamp: time.Now(),
	}
}

func (m *Monitor) startMonitoring() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Test ICMP targets
			for _, target := range m.icmpTargets {
				go func(t string) {
					result := m.ping(t)

					m.mutex.Lock()
					m.results[t] = &result
					m.mutex.Unlock()

					// Broadcast to WebSocket clients
					select {
					case m.broadcast <- result:
					default:
					}
				}(target)
			}

			// Test TCP targets
			for target, port := range m.tcpTargets {
				go func(t string, p int) {
					result := m.tcpTest(t, p)

					// Use a key that includes the protocol to avoid conflicts
					resultKey := fmt.Sprintf("%s-tcp-%d", t, p)

					m.mutex.Lock()
					m.results[resultKey] = &result
					m.mutex.Unlock()

					// Broadcast to WebSocket clients
					select {
					case m.broadcast <- result:
					default:
					}
				}(target, port)
			}
		}
	}
}

func (m *Monitor) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := m.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	// Create a per-connection mutex for synchronized writes
	connMutex := &sync.Mutex{}

	m.clientsMux.Lock()
	m.clients[conn] = connMutex
	m.clientsMux.Unlock()

	defer func() {
		m.clientsMux.Lock()
		delete(m.clients, conn)
		m.clientsMux.Unlock()
	}()

	// Send current results immediately (using the connection mutex)
	m.mutex.RLock()
	for _, result := range m.results {
		connMutex.Lock()
		err := conn.WriteJSON(result)
		connMutex.Unlock()
		if err != nil {
			log.Printf("WebSocket write failed: %v", err)
			m.mutex.RUnlock()
			return
		}
	}
	m.mutex.RUnlock()

	// Keep connection alive and handle incoming messages
	for {
		// Read message to detect client disconnect
		_, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}
	}
}

func (m *Monitor) broadcastResults() {
	for {
		result := <-m.broadcast
		m.clientsMux.RLock()
		for client, connMutex := range m.clients {
			// Write to each client in a separate goroutine to prevent blocking
			// Use the per-connection mutex to prevent concurrent writes
			go func(c *websocket.Conn, mutex *sync.Mutex) {
				mutex.Lock()
				err := c.WriteJSON(result)
				mutex.Unlock()

				if err != nil {
					log.Printf("WebSocket write failed: %v", err)
					c.Close()
					m.clientsMux.Lock()
					delete(m.clients, c)
					m.clientsMux.Unlock()
				}
			}(client, connMutex)
		}
		m.clientsMux.RUnlock()
	}
}

func (m *Monitor) getResults(w http.ResponseWriter, r *http.Request) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(m.results)
}

const htmlTemplate = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>AzNetMon - Network Monitor</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        
        body {
            font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            min-height: 100vh;
            padding: 20px;
        }
        
        .container {
            max-width: 1200px;
            margin: 0 auto;
        }
        
        .header {
            text-align: center;
            color: white;
            margin-bottom: 30px;
        }
        
        .header h1 {
            font-size: 2.5rem;
            margin-bottom: 10px;
            text-shadow: 2px 2px 4px rgba(0,0,0,0.3);
        }
        
        .header p {
            font-size: 1.1rem;
            opacity: 0.9;
        }
        
        .stats {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 20px;
            margin-bottom: 30px;
        }
        
        .stat-card {
            background: rgba(255, 255, 255, 0.1);
            backdrop-filter: blur(10px);
            border-radius: 15px;
            padding: 20px;
            text-align: center;
            color: white;
            border: 1px solid rgba(255, 255, 255, 0.2);
        }
        
        .stat-card h3 {
            font-size: 2rem;
            margin-bottom: 5px;
        }
        
        .stat-card p {
            opacity: 0.8;
        }
        
        .targets-grid {
            display: grid;
            grid-template-columns: repeat(auto-fill, minmax(350px, 1fr));
            gap: 20px;
        }
        
        .target-card {
            background: rgba(255, 255, 255, 0.95);
            border-radius: 15px;
            padding: 25px;
            box-shadow: 0 8px 32px rgba(0, 0, 0, 0.1);
            transition: transform 0.2s ease, box-shadow 0.2s ease;
        }
        
        .target-card:hover {
            transform: translateY(-5px);
            box-shadow: 0 12px 40px rgba(0, 0, 0, 0.15);
        }
        
        .target-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 15px;
        }
        
        .target-name {
            font-size: 1.3rem;
            font-weight: 600;
            color: #333;
        }
        
        .status {
            padding: 8px 16px;
            border-radius: 20px;
            font-weight: 600;
            font-size: 0.9rem;
        }
        
        .status.online {
            background: #d4edda;
            color: #155724;
        }
        
        .status.offline {
            background: #f8d7da;
            color: #721c24;
        }
        
        .target-metrics {
            display: grid;
            grid-template-columns: 1fr 1fr;
            gap: 15px;
            margin-bottom: 15px;
        }
        
        .metric {
            text-align: center;
            padding: 15px;
            background: #f8f9fa;
            border-radius: 10px;
        }
        
        .metric-value {
            font-size: 1.5rem;
            font-weight: 700;
            color: #333;
            margin-bottom: 5px;
        }
        
        .metric-label {
            font-size: 0.9rem;
            color: #666;
        }
        
        .error-message {
            background: #f8d7da;
            color: #721c24;
            padding: 10px;
            border-radius: 8px;
            font-size: 0.9rem;
            margin-top: 10px;
        }
        
        .last-updated {
            text-align: center;
            color: #666;
            font-size: 0.9rem;
            margin-top: 15px;
        }
        
        .protocol-badge {
            display: inline-block;
            background: #e2e3e5;
            color: #383d41;
            padding: 4px 8px;
            border-radius: 4px;
            font-size: 0.8rem;
            margin-left: 8px;
            font-weight: normal;
        }
        
        .protocol-badge.icmp {
            background: #cce5ff;
            color: #004085;
        }
        
        .protocol-badge.tcp {
            background: #d1ecf1;
            color: #0c5460;
        }
        
        .loading {
            text-align: center;
            color: white;
            font-size: 1.2rem;
            margin: 50px 0;
        }
        
        @keyframes pulse {
            0%, 100% { opacity: 1; }
            50% { opacity: 0.5; }
        }
        
        .loading {
            animation: pulse 1.5s infinite;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🌐 AzNetMon</h1>
            <p>Real-time Network Monitoring Dashboard (ICMP & TCP)</p>
        </div>
        
        <div class="stats">
            <div class="stat-card">
                <h3 id="total-targets">-</h3>
                <p>Total Targets</p>
            </div>
            <div class="stat-card">
                <h3 id="online-targets">-</h3>
                <p>Online</p>
            </div>
            <div class="stat-card">
                <h3 id="offline-targets">-</h3>
                <p>Offline</p>
            </div>
            <div class="stat-card">
                <h3 id="avg-latency">-</h3>
                <p>Avg Latency (ms)</p>
            </div>
        </div>
        
        <div id="loading" class="loading">
            Connecting to monitoring service...
        </div>
        
        <div id="targets-container" class="targets-grid" style="display: none;">
        </div>
    </div>

    <script>
        let results = {};
        let socket;
        
        function connect() {
            const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
            socket = new WebSocket(protocol + '//' + window.location.host + '/ws');
            
            socket.onopen = function() {
                console.log('Connected to monitoring service');
                document.getElementById('loading').style.display = 'none';
                document.getElementById('targets-container').style.display = 'grid';
            };
            
            socket.onmessage = function(event) {
                const result = JSON.parse(event.data);
                const key = result.protocol === 'TCP' ? 
                    result.target + '-tcp-' + result.port : 
                    result.target;
                results[key] = result;
                updateDisplay();
            };
            
            socket.onclose = function() {
                console.log('Connection lost, attempting to reconnect...');
                setTimeout(connect, 3000);
            };
            
            socket.onerror = function(error) {
                console.error('WebSocket error:', error);
            };
        }
        
        function updateDisplay() {
            updateStats();
            updateTargets();
        }
        
        function updateStats() {
            const targets = Object.values(results);
            const total = targets.length;
            const online = targets.filter(t => t.success).length;
            const offline = total - online;
            const avgLatency = online > 0 ? 
                (targets.filter(t => t.success).reduce((sum, t) => sum + t.duration_ms, 0) / online).toFixed(1) : 
                '-';
            
            document.getElementById('total-targets').textContent = total;
            document.getElementById('online-targets').textContent = online;
            document.getElementById('offline-targets').textContent = offline;
            document.getElementById('avg-latency').textContent = avgLatency;
        }
        
        function updateTargets() {
            const container = document.getElementById('targets-container');
            container.innerHTML = '';
            
            Object.values(results).forEach(result => {
                const card = createTargetCard(result);
                container.appendChild(card);
            });
        }
        
        function createTargetCard(result) {
            const card = document.createElement('div');
            card.className = 'target-card';
            
            const statusClass = result.success ? 'online' : 'offline';
            const statusText = result.success ? 'Online' : 'Offline';
            const latency = result.success ? result.duration_ms.toFixed(1) : '-';
            const timestamp = new Date(result.timestamp).toLocaleTimeString();
            
            // Create display name based on protocol
            let displayName = result.target;
            if (result.protocol === 'TCP' && result.port) {
                displayName += ':' + result.port;
            }
            
            // Create protocol badge
            const protocolClass = result.protocol.toLowerCase();
            const protocolBadge = '<span class="protocol-badge ' + protocolClass + '">' + result.protocol + '</span>';
            
            card.innerHTML = 
                '<div class="target-header">' +
                    '<div class="target-name">' + displayName + ' ' + protocolBadge + '</div>' +
                    '<div class="status ' + statusClass + '">' + statusText + '</div>' +
                '</div>' +
                '<div class="target-metrics">' +
                    '<div class="metric">' +
                        '<div class="metric-value">' + latency + '</div>' +
                        '<div class="metric-label">Latency (ms)</div>' +
                    '</div>' +
                    '<div class="metric">' +
                        '<div class="metric-value">' + (result.success ? '✓' : '✗') + '</div>' +
                        '<div class="metric-label">Status</div>' +
                    '</div>' +
                '</div>' +
                (result.error ? '<div class="error-message">' + result.error + '</div>' : '') +
                '<div class="last-updated">Last updated: ' + timestamp + '</div>';
            
            return card;
        }
        
        // Initialize connection
        connect();
    </script>
</body>
</html>
`

func main() {
	var icmpTargets string
	var tcpTargets string
	var port int

	flag.StringVar(&icmpTargets, "targets", "", "Comma-separated list of IP addresses or hostnames to monitor with ICMP")
	flag.StringVar(&tcpTargets, "tcp-targets", "", "Comma-separated list of IP:port addresses to monitor with TCP (e.g., google.com:80,example.com:443)")
	flag.IntVar(&port, "port", 8080, "Port to run the web server on")
	flag.Parse()

	// Get targets from environment variables if not provided via flags
	if icmpTargets == "" {
		icmpTargets = os.Getenv("ICMP_TARGETS")
	}
	if tcpTargets == "" {
		tcpTargets = os.Getenv("TCP_TARGETS")
	}

	if icmpTargets == "" && tcpTargets == "" {
		fmt.Println("Error: No targets specified. Use -targets flag for ICMP or -tcp-targets for TCP.")
		fmt.Println("Examples:")
		fmt.Println("  ./aznetmon -targets 8.8.8.8,1.1.1.1,google.com")
		fmt.Println("  ./aznetmon -tcp-targets google.com:80,example.com:443")
		fmt.Println("  ./aznetmon -targets 8.8.8.8 -tcp-targets google.com:80")
		os.Exit(1)
	}

	// Process ICMP targets
	var icmpTargetList []string
	if icmpTargets != "" {
		icmpTargetList = strings.Split(icmpTargets, ",")
		for i, target := range icmpTargetList {
			icmpTargetList[i] = strings.TrimSpace(target)
		}
	}

	// Process TCP targets
	tcpTargetMap := make(map[string]int)
	if tcpTargets != "" {
		tcpTargetList := strings.Split(tcpTargets, ",")
		for _, target := range tcpTargetList {
			target = strings.TrimSpace(target)

			// Parse host:port format
			host, portStr, err := net.SplitHostPort(target)
			if err != nil {
				fmt.Printf("Warning: Invalid TCP target format '%s', skipping. Format should be host:port\n", target)
				continue
			}

			// Convert port to integer
			portNum, err := strconv.Atoi(portStr)
			if err != nil || portNum < 1 || portNum > 65535 {
				fmt.Printf("Warning: Invalid port number '%s' for target '%s', skipping. Port should be 1-65535\n", portStr, host)
				continue
			}

			tcpTargetMap[host] = portNum
		}
	}

	fmt.Printf("Starting AzNetMon on port %d\n", port)
	if len(icmpTargetList) > 0 {
		fmt.Printf("ICMP monitoring targets: %v\n", icmpTargetList)
	}
	if len(tcpTargetMap) > 0 {
		fmt.Printf("TCP monitoring targets: %v\n", tcpTargetMap)
	}

	monitor := NewMonitor(icmpTargetList, tcpTargetMap)

	// Start monitoring in background
	go monitor.startMonitoring()
	go monitor.broadcastResults()

	// Set up HTTP routes
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		tmpl := template.Must(template.New("index").Parse(htmlTemplate))
		tmpl.Execute(w, nil)
	})

	http.HandleFunc("/ws", monitor.handleWebSocket)
	http.HandleFunc("/api/results", monitor.getResults)

	log.Printf("Server starting on :%d", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}
