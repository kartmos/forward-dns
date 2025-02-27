package forwarder

import (
	"context"
	"log"
	"net"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/miekg/dns"
	"github.com/spf13/viper"
)

type Forward struct {
	Port       int
	Forwarding map[string]string
}

func NewForward(path string) (*Forward, error) {
	var Config Forward
	viper.SetConfigFile(path)

	checkConfig(&Config)
	log.Println("[DONE] Set options from config file")

	return &Config, nil
}

func (f *Forward) ListenAndServeDNS() error {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: f.Port})
	if err != nil {
		log.Printf("[WARN] Failed to start server %v: %s\n", conn.LocalAddr(), err)
	}
	log.Printf("UDP-server start on %v", conn.LocalAddr())
	return f.ServeDNS(conn)
}

func (f *Forward) ServeDNS(conn *net.UDPConn) error {
	for {
		buf := make([]byte, 1500)
		n, clientAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Println("[WARN] Err with read UDP request from client")
		}
		log.Printf("[Try] forwarding %v client's request to DNS root Server: %v...\n", clientAddr, f.Forwarding[clientAddr.IP.String()])
		go handleRequest(conn, clientAddr, buf[:n], f.Forwarding[clientAddr.IP.String()])
	}
}

func handleRequest(conn *net.UDPConn, addr *net.UDPAddr, request []byte, dnsServer string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	response, err := forwardToDNSServer(ctx, request, dnsServer)
	if err != nil {
		log.Printf("[DO] Cancel request -> %s", err)
		cancel()
		return
	}

	if _, err := conn.WriteToUDP(response, addr); err != nil {
		log.Printf("[DO] Cancel response -> %s", err)
		cancel()
	}
	log.Printf("[Done] Forward Client: %v -> Sever: %v\n", addr, dnsServer)
}

func checkConfig(config *Forward) {
	viper.OnConfigChange(func(e fsnotify.Event) {
		if e.Op&fsnotify.Write == fsnotify.Write {
			if err := viper.ReadInConfig(); err != nil {
				log.Printf("[WARN] Failed to read config file: %v", err)
				return
			}
			if viper.IsSet("forwarding") {
				config.Forwarding = viper.GetStringMapString("forwarding")
			} else {
				log.Println("[WARN] Key 'forwarding' not found in config")
			}
		}
	})
	viper.WatchConfig()

	if err := viper.ReadInConfig(); err != nil {
		log.Printf("[WARN] Failed to read config file: %v", err)
		return
	}

	config.Port = viper.GetInt("port")
	config.Forwarding = viper.GetStringMapString("forwarding")
}

func forwardToDNSServer(parent context.Context, request []byte, dnsServer string) ([]byte, error) {

	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	msg := new(dns.Msg)
	if err := msg.Unpack(request); err != nil {
		log.Printf("[WARN] Unpack request error %s", err)
		return nil, err
	}

	client := new(dns.Client)

	reply, _, err := client.ExchangeContext(ctx, msg, dnsServer)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.Println("[WARN] DNS request timed out")
		} else {
			log.Printf("[ERROR] DNS exchange failed: %v", err)
		}
		return nil, err
	}

	response, err := reply.Pack()
	if err != nil {
		log.Printf("[WARN] Pack response error %s", err)
		return nil, err
	}
	return response, nil
}
