// Ported from packages/adapter-shared/src/buffer-utils.ts @ 6adca36 (chat v4.40.0).
// Divergences: Node Buffer / ArrayBuffer / Blob → []byte as the only
// expressible member (ArrayBuffer and Blob have no Go analog; those tests
// are permanently deferred). ToBuffer is synchronous for the same reason.
// throwOnUnsupported default-true → DisableThrowOnUnsupported. Unsupported
// types return (nil, *ValidationError) or (nil, nil).
package shared

import "encoding/base64"

const defaultDataURIMime = "application/octet-stream"

// ToBufferOptions is the upstream ToBufferOptions.
type ToBufferOptions struct {
	Platform                  PlatformName
	DisableThrowOnUnsupported bool
}

// ToBuffer converts file data to []byte. []byte is returned as-is.
func ToBuffer(data any, options ToBufferOptions) ([]byte, error) {
	return ToBufferSync(data, options)
}

// ToBufferSync converts non-Blob file data to []byte.
func ToBufferSync(data any, options ToBufferOptions) ([]byte, error) {
	if b, ok := data.([]byte); ok {
		return b, nil
	}
	if options.DisableThrowOnUnsupported {
		return nil, nil
	}
	return nil, NewValidationError(string(options.Platform), "Unsupported file data type")
}

// BufferToDataURI encodes buffer as a data URI. mimeType "" uses
// application/octet-stream.
func BufferToDataURI(buffer []byte, mimeType string) string {
	if mimeType == "" {
		mimeType = defaultDataURIMime
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(buffer)
}
