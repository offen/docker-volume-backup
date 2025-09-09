// Copyright 2022 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

// Portions of this file are taken from package `targz`, Copyright (c) 2014 Fredrik Wallgren
// Licensed under the MIT License: https://github.com/walle/targz/blob/57fe4206da5abf7dd3901b4af3891ec2f08c7b08/LICENSE

package main

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/klauspost/pgzip"
	"github.com/offen/docker-volume-backup/internal/errwrap"
)

func createArchive(files []string, inputFilePath, outputFilePath string, compression string, compressionConcurrency int) error {
	_, outputFilePath, err := makeAbsolute(stripTrailingSlashes(inputFilePath), outputFilePath)
	if err != nil {
		return errwrap.Wrap(err, "error transposing given file paths")
	}
	if err := os.MkdirAll(filepath.Dir(outputFilePath), 0755); err != nil {
		return errwrap.Wrap(err, "error creating output file path")
	}

	if err := compress(files, outputFilePath, compression, compressionConcurrency); err != nil {
		return errwrap.Wrap(err, "error creating archive")
	}

	return nil
}

func stripTrailingSlashes(path string) string {
	if len(path) > 0 && path[len(path)-1] == '/' {
		path = path[0 : len(path)-1]
	}

	return path
}

func makeAbsolute(inputFilePath, outputFilePath string) (string, string, error) {
	inputFilePath, err := filepath.Abs(inputFilePath)
	if err == nil {
		outputFilePath, err = filepath.Abs(outputFilePath)
	}

	return inputFilePath, outputFilePath, err
}

func compress(paths []string, outFilePath, algo string, concurrency int) error {
	file, err := os.Create(outFilePath)
	if err != nil {
		return errwrap.Wrap(err, "error creating out file")
	}

	prefix := path.Dir(outFilePath)
	compressWriter, err := getCompressionWriter(file, algo, concurrency)
	if err != nil {
		return errwrap.Wrap(err, "error getting compression writer")
	}
	tarWriter := tar.NewWriter(compressWriter)

	for _, p := range paths {
		if err := writeTarball(p, tarWriter, prefix); err != nil {
			return errwrap.Wrap(err, fmt.Sprintf("error writing %s to archive", p))
		}
	}

	err = tarWriter.Close()
	if err != nil {
		return errwrap.Wrap(err, "error closing tar writer")
	}

	err = compressWriter.Close()
	if err != nil {
		return errwrap.Wrap(err, "error closing compression writer")
	}

	err = file.Close()
	if err != nil {
		return errwrap.Wrap(err, "error closing file")
	}

	return nil
}

func getCompressionWriter(file *os.File, algo string, concurrency int) (io.WriteCloser, error) {
	switch algo {
	case "none":
		return &passThroughWriteCloser{file}, nil
	case "gz":
		w, err := pgzip.NewWriterLevel(file, 5)
		if err != nil {
			return nil, errwrap.Wrap(err, "gzip error")
		}

		if concurrency == 0 {
			concurrency = runtime.GOMAXPROCS(0)
		}

		if err := w.SetConcurrency(1<<20, concurrency); err != nil {
			return nil, errwrap.Wrap(err, "error setting concurrency")
		}

		return w, nil
	case "zst":
		compressWriter, err := zstd.NewWriter(file)
		if err != nil {
			return nil, errwrap.Wrap(err, "zstd error")
		}
		return compressWriter, nil
	default:
		return nil, errwrap.Wrap(nil, fmt.Sprintf("unsupported compression algorithm: %s", algo))
	}
}

func writeTarball(path string, tarWriter *tar.Writer, prefix string) (returnErr error) {
	fileInfo, err := os.Lstat(path)
	if err != nil {
		returnErr = errwrap.Wrap(err, fmt.Sprintf("error getting file info for %s", path))
		return
	}

	if fileInfo.Mode()&os.ModeSocket == os.ModeSocket {
		return nil
	}

	var link string
	if fileInfo.Mode()&os.ModeSymlink == os.ModeSymlink {
		var err error
		if link, err = os.Readlink(path); err != nil {
			returnErr = errwrap.Wrap(err, fmt.Sprintf("error resolving symlink %s", path))
			return
		}
	}

	header, err := tar.FileInfoHeader(fileInfo, link)
	if err != nil {
		returnErr = errwrap.Wrap(err, "error getting file info header")
		return
	}
	header.Name = strings.TrimPrefix(path, prefix)

	err = tarWriter.WriteHeader(header)
	if err != nil {
		returnErr = errwrap.Wrap(err, "error writing file info header")
		return
	}

	if !fileInfo.Mode().IsRegular() {
		return nil
	}

	file, err := os.Open(path)
	if err != nil {
		returnErr = errwrap.Wrap(err, fmt.Sprintf("error opening %s", path))
		return
	}
	defer func() {
		returnErr = file.Close()
	}()

	_, err = io.Copy(tarWriter, file)
	if err != nil {
		returnErr = errwrap.Wrap(err, fmt.Sprintf("error copying %s to tar writer", path))
		return
	}

	return nil
}

type passThroughWriteCloser struct {
	target io.WriteCloser
}

func (p *passThroughWriteCloser) Write(b []byte) (int, error) {
	return p.target.Write(b)
}

func (p *passThroughWriteCloser) Close() error {
	return nil
}
