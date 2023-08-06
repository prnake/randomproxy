package main

import (
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"strings"

	"github.com/gogf/gf/v2/container/garray"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gctx"
	"github.com/gogf/gf/v2/util/gconv"
)

func randomIPV6FromSubnet(network string) (net.IP, error) {
	_, subnet, err := net.ParseCIDR(network)
	if err != nil {
		return nil, err
	}
	// 获取子网掩码位长度
	ones, _ := subnet.Mask.Size()
	// g.Dump(ones)
	// g.Dump(bits)
	// Get the prefix of the subnet.
	prefix := subnet.IP.To16()
	// println("prefix: ", prefix.String())

	// Seed the random number generator.
	// rand.Seed(time.Now().UnixNano())

	// Generate a random IPv6 address from the subnet.
	for i := ones / 8; i < len(prefix); i++ {
		prefix[i] = byte(rand.Intn(256))
	}

	return prefix, nil
}
func handleTunneling(ctx g.Ctx, w http.ResponseWriter, r *http.Request) {
	var IPS []interface{}
	// 获取域名不带端口
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		g.Log().Error(ctx, err.Error())
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	g.Log().Debug(ctx, "host", host)
	// 根据r.Host获取IP

	serverIP, isipv6, err := getIPAddress(host)
	if err != nil {
		g.Log().Error(ctx, err.Error())
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	if isipv6 {
		g.Log().Debug(ctx, "serverIP", serverIP)
		IPS = g.Cfg().MustGet(ctx, "IP6S").Slice()
	} else {
		g.Log().Debug(ctx, "serverIP", serverIP)
		IPS = g.Cfg().MustGet(ctx, "IPS").Slice()
	}
	if len(IPS) == 0 {
		IPS = g.Cfg().MustGet(ctx, "IPS").Slice()
	}

	IPA := garray.NewArrayFrom(IPS)
	IP, found := IPA.Rand()
	if !found {
		g.Log().Error(ctx, "no ip found")
		http.Error(w, "no ip found", http.StatusServiceUnavailable)
		return
	}
	ip := gconv.String(IP)
	ipv6sub := g.Cfg().MustGet(ctx, "IP6SUB").String()
	if isipv6 && ipv6sub != "" {
		tempIP, _ := randomIPV6FromSubnet(ipv6sub)
		ip = tempIP.String()
	}

	// g.Log().Debug(ctx, "ip", ip)
	dialer := &net.Dialer{
		LocalAddr: &net.TCPAddr{IP: net.ParseIP(ip), Port: 0},
	}
	destConn, err := dialer.Dial("tcp", r.Host)
	if err != nil {
		g.Log().Error(ctx, err.Error())

		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	}
	// g.Dump(clientConn.RemoteAddr().String())

	go transfer(destConn, clientConn)
	go transfer(clientConn, destConn)
	g.Log().Debug(ctx, r.Host, clientConn.RemoteAddr().String(), destConn.RemoteAddr().String(), destConn.LocalAddr().String())
}

func transfer(destination io.WriteCloser, source io.ReadCloser) {
	defer destination.Close()
	defer source.Close()
	io.Copy(destination, source)
}

func handleHTTP(ctx g.Ctx, w http.ResponseWriter, req *http.Request) {
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
func getIPAddress(domain string) (ip string, ipv6 bool, err error) {
	ipAddresses, err := net.LookupHost(domain)
	if err != nil {
		return "", false, err
	}
	for _, ipAddress := range ipAddresses {
		// 如果是地址包含 : 说明是IPV6地址
		if strings.Contains(ipAddress, ":") {
			return ipAddress, true, nil
		}
	}
	return ipAddresses[0], false, nil
}
func main() {
	ctx := gctx.New()

	server := &http.Server{
		Addr: ":31280",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// g.DumpWithType(r.Header)
			if r.Method == http.MethodConnect {
				g.Log().Debug(ctx, "handleTunneling", r.Host)

				handleTunneling(ctx, w, r)
			} else {
				g.Log().Debug(ctx, "handleHTTP", r.Host)
				handleHTTP(ctx, w, r)
			}
		}),
	}

	log.Printf("Starting http/https proxy server on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}
