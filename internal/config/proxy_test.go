package config

import "testing"

func TestDirectProxyHostsFromNoProxyProviders(t *testing.T) {
	t.Skip("No no_proxy providers currently exist")
	spec := Default().NetworkProxySpec()
	hasJiutian := false
	for _, h := range spec.DirectHosts {
		if h == "api.jiutian.10086.cn" {
			hasJiutian = true
		}
	}
	if !hasJiutian {
		t.Errorf("a no_proxy provider's host should land in DirectHosts, got %v", spec.DirectHosts)
	}
}

func TestExplicitProxyOverridesProviderNoProxy(t *testing.T) {
	// An explicit custom proxy (e.g. a mandatory corporate proxy) must apply to
	// every provider, including no_proxy ones like MoMA, so it isn't unreachable
	// behind the proxy (#3635).
	c := Default()
	c.Network.ProxyMode = "custom"
	spec := c.NetworkProxySpec()
	for _, h := range spec.DirectHosts {
		if h == "token-plan-cn.jiutian.10086.cn" {
			t.Fatalf("custom proxy must not force MoMA direct; DirectHosts = %v", spec.DirectHosts)
		}
	}
}
