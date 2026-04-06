package app

import (
	"encoding/json"
)

// jsonFast is the json-iterator instance (will be initialized when dependency is available)
// For now, we use the standard library as a fallback.
var jsonFast = &stdlibAdapter{}

// stdlibAdapter adapts the standard library to our JSON interface
type stdlibAdapter struct{}

func (s *stdlibAdapter) Marshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func (s *stdlibAdapter) MarshalIndent(v interface{}, prefix, indent string) ([]byte, error) {
	return json.MarshalIndent(v, prefix, indent)
}

func (s *stdlibAdapter) Unmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// JSONEncoder wraps json.Encoder for streaming
type JSONEncoder struct {
	enc *json.Encoder
}

func (e *JSONEncoder) Encode(v interface{}) error {
	return e.enc.Encode(v)
}

// JSONDecoder wraps json.Decoder for streaming
type JSONDecoder struct {
	dec *json.Decoder
}

func (d *JSONDecoder) Decode(v interface{}) error {
	return d.dec.Decode(v)
}

// JSONMarshal wraps fast JSON Marshal
func JSONMarshal(v interface{}) ([]byte, error) {
	return jsonFast.Marshal(v)
}

// JSONMarshalIndent wraps fast JSON MarshalIndent
func JSONMarshalIndent(v interface{}, prefix, indent string) ([]byte, error) {
	return jsonFast.MarshalIndent(v, prefix, indent)
}

// JSONUnmarshal wraps fast JSON Unmarshal
func JSONUnmarshal(data []byte, v interface{}) error {
	return jsonFast.Unmarshal(data, v)
}

// NewEncoder creates a new JSON encoder
func NewEncoder(w interface{}) *JSONEncoder {
	// Type assertion for io.Writer
	type ioWriter interface {
		Write(p []byte) (n int, err error)
	}
	if writer, ok := w.(ioWriter); ok {
		return &JSONEncoder{enc: json.NewEncoder(writer)}
	}
	return nil
}

// NewDecoder creates a new JSON decoder
func NewDecoder(r interface{}) *JSONDecoder {
	// Type assertion for io.Reader
	type ioReader interface {
		Read(p []byte) (n int, err error)
	}
	if reader, ok := r.(ioReader); ok {
		return &JSONDecoder{dec: json.NewDecoder(reader)}
	}
	return nil
}
