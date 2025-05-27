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
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

type ICMPResult struct {
	Target    string    `json:"target"`
	Success   bool      `json:"success"`
	Duration  float64   `json:"duration_ms"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

type TargetSummary struct {
	Target     string  `json:"target"`
	TotalTests int     `json:"total_tests"`
	Successful int     `json:"successful"`
	Failed     int     `json:"failed"`
	PacketLoss float64 `json:"packet_loss_percent"`
	AvgLatency float64 `json:"avg_latency_ms"`
	LastError  string  `json:"last_error,omitempty"`
}

type DashboardSummary struct {
	Timestamp      time.Time       `json:"timestamp"`
	TotalTargets   int             `json:"total_targets"`
	OnlineTargets  int             `json:"online_targets"`
	OfflineTargets int             `json:"offline_targets"`
	AvgLatency     float64         `json:"avg_latency_ms"`
	TargetStats    []TargetSummary `json:"target_stats"`
}

type Monitor struct {
	targets         []string
	results         map[string]*ICMPResult
	summaryData     map[string]*TargetSummary
	mutex           sync.RWMutex
	clients         map[*websocket.Conn]*sync.Mutex // Change to store per-connection mutex
	clientsMux      sync.RWMutex
	broadcast       chan ICMPResult
	summaryChan     chan DashboardSummary
	upgrader        websocket.Upgrader
	testDuration    time.Duration
	testEndTime     time.Time
	testEndTimeLock sync.RWMutex
}

func NewMonitor(targets []string, testDuration time.Duration) *Monitor {
	m := &Monitor{
		targets:     targets,
		results:     make(map[string]*ICMPResult),
		summaryData: make(map[string]*TargetSummary),
		clients:     make(map[*websocket.Conn]*sync.Mutex),
		broadcast:   make(chan ICMPResult),
		summaryChan: make(chan DashboardSummary),
		testDuration: testDuration,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins in this simple app
			},
		},
	}
	
	// Initialize summary data for each target
	for _, target := range targets {
		m.summaryData[target] = &TargetSummary{
			Target: target,
		}
	}
	
	// If a test duration was specified, set the end time
	if testDuration > 0 {
		m.testEndTimeLock.Lock()
		m.testEndTime = time.Now().Add(testDuration)
		m.testEndTimeLock.Unlock()
	}
	
	return m
}

func (m *Monitor) ping(target string) ICMPResult {
	start := time.Now()

	// Resolve the address
	dst, err := net.ResolveIPAddr("ip4", target)
	if err != nil {
		return ICMPResult{
			Target:    target,
			Success:   false,
			Duration:  0,
			Error:     fmt.Sprintf("Failed to resolve %s: %v", target, err),
			Timestamp: time.Now(),
		}
	}

	// Create ICMP connection
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return ICMPResult{
			Target:    target,
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
		return ICMPResult{
			Target:    target,
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
		return ICMPResult{
			Target:    target,
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
		return ICMPResult{
			Target:    target,
			Success:   false,
			Duration:  0,
			Error:     fmt.Sprintf("Failed to receive ICMP reply: %v", err),
			Timestamp: time.Now(),
		}
	}

	return ICMPResult{
		Target:    target,
		Success:   true,
		Duration:  float64(duration.Nanoseconds()) / 1e6, // Convert to milliseconds
		Timestamp: time.Now(),
	}
}

func (m *Monitor) startMonitoring() {
	ticker := time.NewTicker(2 * time.Second)
	summaryTicker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	defer summaryTicker.Stop()

	// Send an initial summary
	go m.generateAndSendSummary()

	for {
		select {
		case <-ticker.C:
			// Check if test duration has elapsed
			if m.testDuration > 0 {
				m.testEndTimeLock.RLock()
				endTime := m.testEndTime
				m.testEndTimeLock.RUnlock()
				
				if time.Now().After(endTime) {
					log.Printf("Test duration of %v has elapsed. Monitoring stopped.", m.testDuration)
					return
				}
			}
			
			for _, target := range m.targets {
				go func(t string) {
					result := m.ping(t)

					m.mutex.Lock()
					m.results[t] = &result
					
					// Update summary data
					summary, exists := m.summaryData[t]
					if !exists {
						summary = &TargetSummary{
							Target: t,
						}
						m.summaryData[t] = summary
					}
					
					summary.TotalTests++
					if result.Success {
						summary.Successful++
						// Update rolling average latency
						if summary.AvgLatency == 0 {
							summary.AvgLatency = result.Duration
						} else {
							summary.AvgLatency = (summary.AvgLatency*float64(summary.Successful-1) + result.Duration) / float64(summary.Successful)
						}
					} else {
						summary.Failed++
						summary.LastError = result.Error
					}
					
					// Calculate packet loss percentage
					summary.PacketLoss = float64(summary.Failed) / float64(summary.TotalTests) * 100
					
					m.mutex.Unlock()

					// Broadcast to WebSocket clients
					select {
					case m.broadcast <- result:
					default:
					}
				}(target)
			}
		case <-summaryTicker.C:
			go m.generateAndSendSummary()
		}
	}
}

func (m *Monitor) generateAndSendSummary() {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	summary := DashboardSummary{
		Timestamp:    time.Now(),
		TotalTargets: len(m.targets),
		TargetStats:  make([]TargetSummary, 0, len(m.targets)),
	}
	
	// Copy target summaries
	var totalOnline int
	var totalLatency float64
	
	for _, targetSummary := range m.summaryData {
		summary.TargetStats = append(summary.TargetStats, *targetSummary)
		
		// Count online targets (those with the most recent ping successful)
		targetResult, exists := m.results[targetSummary.Target]
		if exists && targetResult.Success {
			totalOnline++
			totalLatency += targetSummary.AvgLatency
		}
	}
	
	summary.OnlineTargets = totalOnline
	summary.OfflineTargets = len(m.targets) - totalOnline
	
	if totalOnline > 0 {
		summary.AvgLatency = totalLatency / float64(totalOnline)
	}
	
	// Send to all WebSocket clients
	select {
	case m.summaryChan <- summary:
	default:
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
		select {
		case result := <-m.broadcast:
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
			
		case summary := <-m.summaryChan:
			m.clientsMux.RLock()
			for client, connMutex := range m.clients {
				go func(c *websocket.Conn, mutex *sync.Mutex) {
					mutex.Lock()
					err := c.WriteJSON(map[string]interface{}{
						"type":    "summary",
						"summary": summary,
					})
					mutex.Unlock()

					if err != nil {
						log.Printf("WebSocket summary write failed: %v", err)
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
    <title>AzNetMon - ICMP Monitor</title>
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
        
        .summary-info {
            text-align: center;
            color: white;
            background: rgba(255, 255, 255, 0.1);
            backdrop-filter: blur(5px);
            border-radius: 10px;
            padding: 10px;
            margin-bottom: 20px;
            border: 1px solid rgba(255, 255, 255, 0.2);
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🌐 AzNetMon</h1>
            <p>Real-time ICMP Network Monitoring Dashboard</p>
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
        
        <div id="summary-info" class="summary-info">
            <p id="last-summary-update">Waiting for summary data...</p>
        </div>
        
        <div id="loading" class="loading">
            Connecting to monitoring service...
        </div>
        
        <div id="targets-container" class="targets-grid" style="display: none;">
        </div>
    </div>

    <script>
        let results = {};
        let summaryData = null;
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
                const data = JSON.parse(event.data);
                
                // Check if this is a summary message
                if (data.type === 'summary') {
                    summaryData = data.summary;
                    updateDisplay();
                    // Add timestamp for the last summary update
                    const timestamp = new Date(summaryData.timestamp);
                    document.getElementById('last-summary-update').textContent = 'Last summary update: ' + timestamp.toLocaleTimeString();
                } else {
                    // Regular ping result
                    results[data.target] = data;
                    updateDisplay();
                }
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
            // If we have summary data, use it for the stats
            if (summaryData) {
                document.getElementById('total-targets').textContent = summaryData.total_targets;
                document.getElementById('online-targets').textContent = summaryData.online_targets;
                document.getElementById('offline-targets').textContent = summaryData.offline_targets;
                document.getElementById('avg-latency').textContent = summaryData.avg_latency_ms.toFixed(1);
                return;
            }
            
            // Fall back to calculating from individual results if no summary available
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
            
            // If we have summary data, use it to enhance the target cards
            if (summaryData) {
                summaryData.target_stats.forEach(targetSummary => {
                    // Find the corresponding current result
                    const currentResult = results[targetSummary.target];
                    
                    // Create a card that combines current status with summary data
                    const card = createTargetCardWithSummary(currentResult, targetSummary);
                    container.appendChild(card);
                });
            } else {
                // Fall back to basic display using only current results
                Object.values(results).forEach(result => {
                    const card = createTargetCard(result);
                    container.appendChild(card);
                });
            }
        }
        
        function createTargetCardWithSummary(result, summary) {
            const card = document.createElement('div');
            card.className = 'target-card';
            
            // Determine current status
            const currentStatusClass = result && result.success ? 'online' : 'offline';
            const currentStatusText = result && result.success ? 'Online' : 'Offline';
            const currentLatency = result && result.success ? result.duration_ms.toFixed(1) : '-';
            const timestamp = result ? new Date(result.timestamp).toLocaleTimeString() : '-';
            
            card.innerHTML = 
                '<div class="target-header">' +
                    '<div class="target-name">' + summary.target + '</div>' +
                    '<div class="status ' + currentStatusClass + '">' + currentStatusText + '</div>' +
                '</div>' +
                '<div class="target-metrics">' +
                    '<div class="metric">' +
                        '<div class="metric-value">' + currentLatency + '</div>' +
                        '<div class="metric-label">Current Latency (ms)</div>' +
                    '</div>' +
                    '<div class="metric">' +
                        '<div class="metric-value">' + summary.avg_latency_ms.toFixed(1) + '</div>' +
                        '<div class="metric-label">Avg Latency (ms)</div>' +
                    '</div>' +
                '</div>' +
                '<div class="target-metrics">' +
                    '<div class="metric">' +
                        '<div class="metric-value">' + summary.packet_loss_percent.toFixed(1) + '%</div>' +
                        '<div class="metric-label">Packet Loss</div>' +
                    '</div>' +
                    '<div class="metric">' +
                        '<div class="metric-value">' + summary.successful + '/' + summary.total_tests + '</div>' +
                        '<div class="metric-label">Success Rate</div>' +
                    '</div>' +
                '</div>' +
                (result && result.error ? '<div class="error-message">' + result.error + '</div>' : '') +
                (summary.last_error && (!result || !result.error) ? '<div class="error-message">Last error: ' + summary.last_error + '</div>' : '') +
                '<div class="last-updated">Last updated: ' + timestamp + '</div>';
            
            return card;
        }
        
        function createTargetCard(result) {
            const card = document.createElement('div');
            card.className = 'target-card';
            
            const statusClass = result.success ? 'online' : 'offline';
            const statusText = result.success ? 'Online' : 'Offline';
            const latency = result.success ? result.duration_ms.toFixed(1) : '-';
            const timestamp = new Date(result.timestamp).toLocaleTimeString();
            
            card.innerHTML = 
                '<div class="target-header">' +
                    '<div class="target-name">' + result.target + '</div>' +
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
	var targets string
	var port int
	var duration string

	flag.StringVar(&targets, "targets", "", "Comma-separated list of IP addresses or hostnames to monitor")
	flag.IntVar(&port, "port", 8080, "Port to run the web server on")
	flag.StringVar(&duration, "duration", "", "Duration for ICMP testing (e.g., 1h, 30m, 24h). If not specified, testing continues indefinitely")
	flag.Parse()

	// Get targets from environment variable if not provided via flag
	if targets == "" {
		targets = os.Getenv("ICMP_TARGETS")
	}
	
	// Get duration from environment variable if not provided via flag
	if duration == "" {
		duration = os.Getenv("ICMP_DURATION")
	}

	if targets == "" {
		fmt.Println("Error: No targets specified. Use -targets flag or ICMP_TARGETS environment variable.")
		fmt.Println("Example: ./aznetmon -targets 8.8.8.8,1.1.1.1,google.com")
		os.Exit(1)
	}

	targetList := strings.Split(targets, ",")
	for i, target := range targetList {
		targetList[i] = strings.TrimSpace(target)
	}

	// Parse duration if provided
	var testDuration time.Duration
	if duration != "" {
		var err error
		testDuration, err = time.ParseDuration(duration)
		if err != nil {
			fmt.Printf("Error: Invalid duration format '%s': %v\n", duration, err)
			fmt.Println("Valid examples: 30m, 2h, 24h, 7d")
			os.Exit(1)
		}
		fmt.Printf("ICMP testing will run for: %v\n", testDuration)
	} else {
		fmt.Println("ICMP testing will run continuously (no duration specified)")
	}

	fmt.Printf("Starting AzNetMon on port %d\n", port)
	fmt.Printf("Monitoring targets: %v\n", targetList)

	monitor := NewMonitor(targetList, testDuration)

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
