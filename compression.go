// Copyright 2024 Pieter Berkel
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package storageredis

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"fmt"
	"io"
)

const maxDecompressedBytes int64 = 4 << 20 // 4 MiB

// compress compresses input using the algorithm specified by rs.Compression.
func (rs *RedisStorage) compress(input []byte) ([]byte, error) {
	if rs.Compression == CompressionZlib {
		return compressWriter(input, func(w io.Writer) (io.WriteCloser, error) {
			return zlib.NewWriter(w), nil
		})
	}
	return compressWriter(input, func(w io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(w, flate.DefaultCompression)
	})
}

// decompress decompresses input using the algorithm identified by compressionFlag,
// which is the value stored in StorageData.Compression for the given key.
// This allows flate- and zlib-compressed values to coexist in Redis without migration.
func (rs *RedisStorage) decompress(input []byte, compressionFlag int) ([]byte, error) {
	if compressionFlag == storageCompressionZlib {
		reader, err := zlib.NewReader(bytes.NewReader(input))
		if err != nil {
			return nil, err
		}
		return decompressReader(reader)
	}
	return decompressReader(flate.NewReader(bytes.NewReader(input)))
}

// compressWriter compresses input by writing it through a writer constructed by newWriter.
// This shared helper eliminates duplication between the flate and zlib compress paths.
func compressWriter(input []byte, newWriter func(io.Writer) (io.WriteCloser, error)) ([]byte, error) {
	var buf bytes.Buffer
	writer, err := newWriter(&buf)
	if err != nil {
		return nil, err
	}
	if _, err = writer.Write(input); err != nil {
		return nil, err
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decompressReader reads all decompressed bytes from reader, enforcing the 4 MiB limit
// to guard against decompression bombs. Used by both flate and zlib decompress paths.
func decompressReader(reader io.ReadCloser) ([]byte, error) {
	defer reader.Close()
	// Read one byte beyond the limit so we can detect if the limit was exceeded.
	limitedReader := io.LimitReader(reader, maxDecompressedBytes+1)
	output, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, err
	}
	if int64(len(output)) > maxDecompressedBytes {
		return nil, fmt.Errorf("decompressed value exceeds limit (%d bytes)", maxDecompressedBytes)
	}
	return output, nil
}
