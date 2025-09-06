package main

import (
    "log"
    "net"
    "os"
    "os/signal"
    "strconv"
    "strings"
    "syscall"
    "time"

    "github.com/miekg/dns"
)

// Configuration with environment variables and defaults
type Config struct {
    ListenAddr     string
    ListenPort     string
    DockerDNS      string
    UpstreamDNS    string
    EnableUpstream bool
    Timeout        time.Duration
    LogLevel       string
    EnableMetrics  bool
    StripSuffix    string
}

func loadConfig() *Config {
    return &Config{
        ListenAddr:     getEnv("LISTEN_ADDR", "127.0.0.1"),
        ListenPort:     getEnv("LISTEN_PORT", "5353"),
        DockerDNS:      getEnv("DOCKER_DNS", "127.0.0.11:53"),
        UpstreamDNS:    getEnv("UPSTREAM_DNS", "8.8.8.8:53"),
        EnableUpstream: getBoolEnv("ENABLE_UPSTREAM", false),
        Timeout:        getDurationEnv("TIMEOUT_SECONDS", 2) * time.Second,
        LogLevel:       getEnv("LOG_LEVEL", "INFO"),
        EnableMetrics:  getBoolEnv("ENABLE_METRICS", false),
        StripSuffix:    getEnv("STRIP_SUFFIX", ".docker"),
    }
}

func getEnv(key, defaultValue string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return defaultValue
}

func getBoolEnv(key string, defaultValue bool) bool {
    if value := os.Getenv(key); value != "" {
        if parsed, err := strconv.ParseBool(value); err == nil {
            return parsed
        }
        log.Printf("Warning: Invalid boolean value for %s: %s, using default: %v", key, value, defaultValue)
    }
    return defaultValue
}

func getDurationEnv(key string, defaultSeconds int) time.Duration {
    if value := os.Getenv(key); value != "" {
        if parsed, err := strconv.Atoi(value); err == nil {
            return time.Duration(parsed)
        }
        log.Printf("Warning: Invalid duration value for %s: %s, using default: %d", key, value, defaultSeconds)
    }
    return time.Duration(defaultSeconds)
}

type DNSProxy struct {
    config         *Config
    dockerClient   *dns.Client
    upstreamClient *dns.Client
    queryCount     int64
    errorCount     int64
}

func NewDNSProxy(config *Config) *DNSProxy {
    return &DNSProxy{
        config: config,
        dockerClient: &dns.Client{
            Net:     "udp",
            Timeout: config.Timeout,
        },
        upstreamClient: &dns.Client{
            Net:     "udp",
            Timeout: config.Timeout,
        },
    }
}

func (p *DNSProxy) logDebug(format string, v ...interface{}) {
    if p.config.LogLevel == "DEBUG" {
        log.Printf("[DEBUG] "+format, v...)
    }
}

func (p *DNSProxy) logInfo(format string, v ...interface{}) {
    if p.config.LogLevel == "DEBUG" || p.config.LogLevel == "INFO" {
        log.Printf("[INFO] "+format, v...)
    }
}

func (p *DNSProxy) logError(format string, v ...interface{}) {
    log.Printf("[ERROR] "+format, v...)
    p.errorCount++
}

func (p *DNSProxy) handleRequest(w dns.ResponseWriter, r *dns.Msg) {
    p.queryCount++
    
    if len(r.Question) == 0 {
        p.logError("Received query with no questions")
        dns.HandleFailed(w, r)
        return
    }

    question := r.Question[0]
    domain := strings.ToLower(question.Name)
    
    p.logInfo("Query #%d for: %s (type: %s) from %s", 
        p.queryCount, domain, dns.TypeToString[question.Qtype], w.RemoteAddr())

    m := new(dns.Msg)
    m.SetReply(r)
    m.Authoritative = false
    m.RecursionAvailable = true

    // Check if domain ends with our configured suffix
    suffix := p.config.StripSuffix + "."
    if strings.HasSuffix(domain, suffix) {
        hostname := strings.TrimSuffix(domain, suffix)
        if hostname == "" {
            p.logError("Empty hostname after stripping suffix from: %s", domain)
            dns.HandleFailed(w, r)
            return
        }

        p.logDebug("Stripping suffix '%s' from '%s', querying Docker DNS for: %s", 
            p.config.StripSuffix, domain, hostname)
        
        if p.queryDockerDNS(m, hostname, question.Qtype) {
            // Update the answer records to have original domain name
            for i := range m.Answer {
                m.Answer[i].Header().Name = domain
            }
            p.logDebug("Successfully resolved %s via Docker DNS", domain)
        } else {
            p.logDebug("No answer from Docker DNS for: %s", hostname)
            m.SetRcode(r, dns.RcodeNameError)
        }
    } else if p.config.EnableUpstream {
        p.logDebug("Forwarding to upstream DNS: %s", domain)
        p.forwardToUpstream(m, r)
    } else {
        p.logDebug("Upstream DNS disabled, returning NXDOMAIN for: %s", domain)
        m.SetRcode(r, dns.RcodeNameError)
    }

    err := w.WriteMsg(m)
    if err != nil {
        p.logError("Error writing response: %v", err)
    }
}

func (p *DNSProxy) queryDockerDNS(response *dns.Msg, hostname string, qtype uint16) bool {
    query := new(dns.Msg)
    query.SetQuestion(dns.Fqdn(hostname), qtype)
    query.RecursionDesired = true

    p.logDebug("Querying Docker DNS %s for: %s", p.config.DockerDNS, hostname)
    reply, _, err := p.dockerClient.Exchange(query, p.config.DockerDNS)
    if err != nil {
        p.logError("Docker DNS query failed for %s: %v", hostname, err)
        return false
    }

    if reply.Rcode != dns.RcodeSuccess {
        p.logDebug("Docker DNS returned error for %s: %s", hostname, dns.RcodeToString[reply.Rcode])
        return false
    }

    if len(reply.Answer) == 0 {
        p.logDebug("No answer from Docker DNS for: %s", hostname)
        return false
    }

    response.Answer = make([]dns.RR, len(reply.Answer))
    copy(response.Answer, reply.Answer)
    
    p.logDebug("Got %d answers from Docker DNS for %s", len(reply.Answer), hostname)
    return true
}

func (p *DNSProxy) forwardToUpstream(response *dns.Msg, request *dns.Msg) {
    domain := request.Question[0].Name
    p.logDebug("Querying upstream DNS %s for: %s", p.config.UpstreamDNS, domain)
    
    reply, _, err := p.upstreamClient.Exchange(request, p.config.UpstreamDNS)
    if err != nil {
        p.logError("Upstream DNS query failed for %s: %v", domain, err)
        response.SetRcode(request, dns.RcodeServerFailure)
        return
    }

    response.Answer = reply.Answer
    response.Ns = reply.Ns
    response.Extra = reply.Extra
    response.SetRcode(request, reply.Rcode)
    
    p.logDebug("Upstream DNS returned %d answers for %s", len(reply.Answer), domain)
}

func (p *DNSProxy) printStats() {
    if p.config.EnableMetrics {
        log.Printf("[METRICS] Total queries: %d, Errors: %d", p.queryCount, p.errorCount)
    }
}

func printConfig(config *Config) {
    log.Printf("=== DNS Proxy Configuration ===")
    log.Printf("Listen Address:    %s:%s", config.ListenAddr, config.ListenPort)
    log.Printf("Docker DNS:        %s", config.DockerDNS)
    if config.EnableUpstream {
        log.Printf("Upstream DNS:      %s", config.UpstreamDNS)
    } else {
        log.Printf("Upstream DNS:      DISABLED")
    }
    log.Printf("Timeout:           %v", config.Timeout)
    log.Printf("Log Level:         %s", config.LogLevel)
    log.Printf("Strip Suffix:      %s", config.StripSuffix)
    log.Printf("Enable Metrics:    %v", config.EnableMetrics)
    log.Printf("==============================")
}

func main() {
    log.SetFlags(log.LstdFlags | log.Lshortfile)
    
    config := loadConfig()
    printConfig(config)

    proxy := NewDNSProxy(config)
    dns.HandleFunc(".", proxy.handleRequest)

    server := &dns.Server{
        Addr: net.JoinHostPort(config.ListenAddr, config.ListenPort),
        Net:  "udp",
    }

    // Graceful shutdown
    c := make(chan os.Signal, 1)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)

    // Optional metrics ticker
    if config.EnableMetrics {
        ticker := time.NewTicker(30 * time.Second)
        go func() {
            for range ticker.C {
                proxy.printStats()
            }
        }()
    }

    go func() {
        <-c
        log.Println("Received shutdown signal...")
        proxy.printStats()
        log.Println("Shutting down DNS server...")
        server.Shutdown()
        os.Exit(0)
    }()

    log.Printf("DNS proxy server starting on %s:%s", config.ListenAddr, config.ListenPort)
    err := server.ListenAndServe()
    if err != nil {
        log.Fatalf("Failed to start server: %v", err)
    }
}