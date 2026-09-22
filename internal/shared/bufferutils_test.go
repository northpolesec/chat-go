package shared

import (
	"errors"
	"regexp"
	"testing"

	"github.com/shoenig/test/must"
)

var dataURIPNGPrefix = regexp.MustCompile(`^data:image/png;base64,`)

func TestToBuffer(t *testing.T) {
	t.Parallel()

	t.Run("returns Buffer unchanged", func(t *testing.T) {
		t.Parallel()
		input := []byte("hello")
		result, err := ToBuffer(input, ToBufferOptions{Platform: PlatformSlack})
		must.NoError(t, err)
		must.True(t, sameBytes(input, result))
	})

	t.Run("throws ValidationError for unsupported type by default", func(t *testing.T) {
		t.Parallel()
		for _, data := range []any{"string", 123, struct{}{}, nil} {
			_, err := ToBuffer(data, ToBufferOptions{Platform: PlatformSlack})
			var ve *ValidationError
			must.True(t, errors.As(err, &ve))
		}
	})

	t.Run("returns null for unsupported type when throwOnUnsupported is false", func(t *testing.T) {
		t.Parallel()
		result, err := ToBuffer("string", ToBufferOptions{
			Platform:                  PlatformTeams,
			DisableThrowOnUnsupported: true,
		})
		must.NoError(t, err)
		must.Nil(t, result)
	})

	t.Run("includes platform in error message", func(t *testing.T) {
		t.Parallel()
		_, err := ToBuffer("invalid", ToBufferOptions{Platform: PlatformSlack})
		var ve *ValidationError
		must.True(t, errors.As(err, &ve))
		must.Eq(t, "slack", ve.Adapter)
	})
}

func TestToBufferSync(t *testing.T) {
	t.Parallel()

	t.Run("returns Buffer unchanged", func(t *testing.T) {
		t.Parallel()
		input := []byte("hello")
		result, err := ToBufferSync(input, ToBufferOptions{Platform: PlatformSlack})
		must.NoError(t, err)
		must.True(t, sameBytes(input, result))
	})

	t.Run("throws ValidationError for unsupported type by default", func(t *testing.T) {
		t.Parallel()
		_, err := ToBufferSync("string", ToBufferOptions{Platform: PlatformSlack})
		var ve *ValidationError
		must.True(t, errors.As(err, &ve))
	})

	t.Run("returns null for unsupported type when throwOnUnsupported is false", func(t *testing.T) {
		t.Parallel()
		result, err := ToBufferSync("string", ToBufferOptions{
			Platform:                  PlatformTeams,
			DisableThrowOnUnsupported: true,
		})
		must.NoError(t, err)
		must.Nil(t, result)
	})
}

func TestBufferToDataUri(t *testing.T) {
	t.Parallel()

	t.Run("converts buffer to data URI with default mime type", func(t *testing.T) {
		t.Parallel()
		result := BufferToDataURI([]byte("hello"), "")
		must.Eq(t, "data:application/octet-stream;base64,aGVsbG8=", result)
	})

	t.Run("converts buffer to data URI with custom mime type", func(t *testing.T) {
		t.Parallel()
		result := BufferToDataURI([]byte("hello"), "text/plain")
		must.Eq(t, "data:text/plain;base64,aGVsbG8=", result)
	})

	t.Run("handles image mime types", func(t *testing.T) {
		t.Parallel()
		result := BufferToDataURI([]byte{0x89, 0x50, 0x4e, 0x47}, "image/png")
		must.True(t, dataURIPNGPrefix.MatchString(result))
	})

	t.Run("handles empty buffer", func(t *testing.T) {
		t.Parallel()
		result := BufferToDataURI([]byte{}, "")
		must.Eq(t, "data:application/octet-stream;base64,", result)
	})
}

func sameBytes(a, b []byte) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}
