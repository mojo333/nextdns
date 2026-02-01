package arp

import (
	"context"
	"net"
	"sync/atomic"
	"time"
)

type cache struct {
	lastUpdate int64
	table      atomic.Value
	ctx        context.Context
	cancel     context.CancelFunc
}

func (c *cache) get() Table {
	now := time.Now().UTC().Unix()
	last := atomic.LoadInt64(&c.lastUpdate)
	if now-last > 30 && atomic.SwapInt64(&c.lastUpdate, now) == last {
		go func() {
			if c.ctx != nil {
				select {
				case <-c.ctx.Done():
					return
				default:
				}
			}
			t, _ := Get()
			c.table.Store(t)
		}()
	}
	t, _ := c.table.Load().(Table)
	return t
}

func (c *cache) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
}

func newCache() *cache {
	ctx, cancel := context.WithCancel(context.Background())
	return &cache{
		ctx:    ctx,
		cancel: cancel,
	}
}

var global = newCache()

func Stop() {
	global.Stop()
}

func SearchMAC(ip net.IP) net.HardwareAddr {
	return global.get().SearchMAC(ip)
}

func SearchIP(mac net.HardwareAddr) net.IP {
	return global.get().SearchIP(mac)
}
