package middleware

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAgentGracePathWhitelist(t *testing.T) {
	tests := []struct {
		path    string
		allowed bool
	}{
		{path: "/api/user/agent/commissions", allowed: true},
		{path: "/api/user/agent/withdraw", allowed: true},
		{path: "/api/user/agent/withdraws", allowed: true},
		{path: "/api/user/agent/withdraw/cancel", allowed: true},
		{path: "/api/user/agent/commission/convert", allowed: false},
		{path: "/api/user/agent/users", allowed: false},
		{path: "/api/user/agent/users/stats", allowed: false},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			assert.Equal(t, test.allowed, isAgentGracePath(test.path))
		})
	}
}
