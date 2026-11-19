package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sorotrail/sorovista/internal/source"
)

func TestNewBrokerWithBuffer(t *testing.T) {
	// The happy path (a positive capacity) is exercised elsewhere; these cases
	// cover the boundaries and invalid input that fall back to the default.
	tests := []struct {
		name     string
		bufSize  int
		wantSize int
	}{
		{"single-slot boundary", 1, 1},
		{"zero falls back to default", 0, DefaultSubscriberBufferSize},
		{"negative falls back to default", -5, DefaultSubscriberBufferSize},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBrokerWithBuffer(tt.bufSize)
			require.NotNil(t, b)
			assert.Equal(t, tt.wantSize, b.bufSize)
			assert.NotNil(t, b.subscribers)
			assert.Equal(t, 0, b.SubscriberCount())

			// The configured capacity must reach the per-subscriber channel.
			ch, cancel := b.Subscribe(source.EventQuery{})
			defer cancel()
			assert.Equal(t, tt.wantSize, cap(ch))
		})
	}
}
