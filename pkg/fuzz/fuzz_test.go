package fuzz

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FuzzGoldenDeserializer asserts that Deserialize never panics: invalid input
// must come back as an error. Documents that do parse are also rendered, so the
// interpolation engine sees fixture-controlled templates and values.
func FuzzGoldenDeserializer(f *testing.F) {
	for _, seed := range goldenSeeds {
		f.Add(seed.data)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		checkGoldenDeserializer(t, data)
	})
}

// FuzzInterpolate asserts that Interpolate never panics on arbitrary template
// text and values, and that any successful result stays within the output cap.
func FuzzInterpolate(f *testing.F) {
	for _, seed := range templateSeeds {
		f.Add(seed.tmpl, seed.value)
	}

	f.Fuzz(func(t *testing.T, tmpl, value string) {
		checkInterpolate(t, tmpl, value)
	})
}

// TestGoldenDeserializerSeeds runs every seed through the same body the fuzz
// target uses. It catches a harness that is wired up wrong (bad seed, wrong
// package, assertion that cannot hold) before fuzzing starts, and pins which
// seeds are expected to parse.
func TestGoldenDeserializerSeeds(t *testing.T) {
	require.NotEmpty(t, goldenSeeds)

	for _, seed := range goldenSeeds {
		t.Run(seed.name, func(t *testing.T) {
			doc, err := checkGoldenDeserializer(t, seed.data)
			if !seed.valid {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.NotEmpty(t, doc.Cases)
		})
	}
}

func TestInterpolateSeeds(t *testing.T) {
	require.NotEmpty(t, templateSeeds)

	for _, seed := range templateSeeds {
		t.Run(seed.name, func(t *testing.T) {
			// checkInterpolate only fails the test on a panic or a breach of
			// the output cap; an error for malformed template text is valid.
			_, _ = checkInterpolate(t, seed.tmpl, seed.value)
		})
	}
}
