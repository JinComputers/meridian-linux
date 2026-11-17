package engine

import (
	"runtime"
	"strings"
)

// The platform is a client declaration, not hardware attestation. Old gateways
// ignore this optional capability; unsupported OSes do not assert a platform.
func clientPlatformCaps(caps string) string { return capsWithPlatform(caps, runtime.GOOS) }

func capsWithPlatform(caps, platform string) string {
	fields := make([]string, 0, 4)
	for _, field := range strings.Split(caps, ",") {
		if field == "" || strings.HasPrefix(field, "platform=") {
			continue
		}
		fields = append(fields, field)
	}
	switch platform {
	case "android", "ios", "linux", "windows":
	case "darwin":
		platform = "macos"
	default:
		return strings.Join(fields, ",")
	}
	return strings.Join(append(fields, "platform="+platform), ",")
}
