// Copyright 2022 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

// Portions of this file are taken from package `targz`, Copyright (c) 2014 Fredrik Wallgren
// Licensed under the MIT License: https://github.com/walle/targz/blob/57fe4206da5abf7dd3901b4af3891ec2f08c7b08/LICENSE

package main

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/klauspost/pgzip"
	"github.com/offen/docker-volume-backup/internal/errwrap"
)

type archiveOptions struct {
	skipMissingFiles  bool
	sourceRoot        string
	sourceRootInfo    os.FileInfo
	sourceDirectories map[string]os.FileInfo
	logger            *slog.Logger
	operations        archiveFileOperations
}

type archiveFileOperations struct {
	lstat    func(string) (os.FileInfo, error)
	readlink func(string) (string, error)
	open     func(string) (*os.File, error)
	create   func(string) (io.WriteCloser, error)
}

func defaultArchiveFileOperations() archiveFileOperations {
	return archiveFileOperations{
		lstat:    os.Lstat,
		readlink: os.Readlink,
		open:     os.Open,
		create:   func(path string) (io.WriteCloser, error) { return os.Create(path) },
	}
}

func (o archiveOptions) skipMissing(path string, err error, operations archiveFileOperations) bool {
	if !o.skipMissingFiles || path == o.sourceRoot || !errors.Is(err, os.ErrNotExist) {
		return false
	}
	if _, isDirectory := o.sourceDirectories[path]; isDirectory {
		return false
	}
	if len(o.sourceDirectories) > 0 {
		for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
			if original, tracked := o.sourceDirectories[parent]; tracked {
				current, statErr := operations.lstat(parent)
				if statErr != nil || !os.SameFile(original, current) {
					return false
				}
			}
			if parent == o.sourceRoot || parent == filepath.Dir(parent) {
				break
			}
		}
	}
	o.logger.Warn("Skipping missing backup entry", "path", path)
	return true
}

func createArchive(files []string, inputFilePath, outputFilePath string, compression string, compressionConcurrency int, options archiveOptions) error {
	inputFilePath, outputFilePath, err := makeAbsolute(stripTrailingSlashes(inputFilePath), outputFilePath)
	if err != nil {
		return errwrap.Wrap(err, "error transposing given file paths")
	}
	if err := os.MkdirAll(filepath.Dir(outputFilePath), 0755); err != nil {
		return errwrap.Wrap(err, "error creating output file path")
	}

	options.sourceRoot = inputFilePath
	if err := compress(files, outputFilePath, compression, compressionConcurrency, options); err != nil {
		return errwrap.Wrap(err, "error compressing archive")
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

func compress(paths []string, outFilePath, algo string, concurrency int, options archiveOptions) (retErr error) {
	operations := options.operations
	if operations.lstat == nil {
		operations = defaultArchiveFileOperations()
	}
	file, err := operations.create(outFilePath)
	if err != nil {
		retErr = errwrap.Wrap(err, "error creating out file")
		return
	}
	defer func() {
		retErr = errors.Join(retErr, file.Close())
	}()

	prefix := path.Dir(outFilePath)
	compressWriter, err := getCompressionWriter(file, algo, concurrency)
	if err != nil {
		retErr = errwrap.Wrap(err, "error getting compression writer")
		return
	}
	tarWriter := tar.NewWriter(compressWriter)

	for _, p := range paths {
		if err := writeTarball(p, tarWriter, prefix, options, operations); err != nil {
			retErr = errwrap.Wrap(err, fmt.Sprintf("error writing %s to archive", p))
			return
		}
	}

	if err := tarWriter.Close(); err != nil {
		retErr = errwrap.Wrap(err, "error closing tar writer")
		return
	}

	if err := compressWriter.Close(); err != nil {
		retErr = errwrap.Wrap(err, "error closing compression writer")
		return
	}

	return
}

func getCompressionWriter(file io.WriteCloser, algo string, concurrency int) (io.WriteCloser, error) {
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

func writeTarball(path string, tarWriter *tar.Writer, prefix string, options archiveOptions, operations archiveFileOperations) (returnErr error) {
	fileInfo, err := operations.lstat(path)
	if err != nil {
		if options.skipMissing(path, err, operations) {
			return nil
		}
		returnErr = errwrap.Wrap(err, fmt.Sprintf("error getting file info for %s", path))
		return
	}
	if path == options.sourceRoot && options.sourceRootInfo != nil && !os.SameFile(options.sourceRootInfo, fileInfo) {
		returnErr = errwrap.Wrap(nil, "backup source root changed while creating archive")
		return
	}
	if original, tracked := options.sourceDirectories[path]; tracked && !os.SameFile(original, fileInfo) {
		returnErr = errwrap.Wrap(nil, fmt.Sprintf("backup source directory changed while creating archive: %s", path))
		return
	}

	if fileInfo.Mode()&os.ModeSocket == os.ModeSocket {
		return nil
	}

	var link string
	if fileInfo.Mode()&os.ModeSymlink == os.ModeSymlink {
		var err error
		if link, err = operations.readlink(path); err != nil {
			if options.skipMissing(path, err, operations) {
				return nil
			}
			returnErr = errwrap.Wrap(err, fmt.Sprintf("error resolving symlink %s", path))
			return
		}
	}

	var file *os.File
	if fileInfo.Mode().IsRegular() {
		file, err = operations.open(path)
		if err != nil {
			if options.skipMissing(path, err, operations) {
				return nil
			}
			returnErr = errwrap.Wrap(err, fmt.Sprintf("error opening %s", path))
			return
		}
		defer func() {
			returnErr = errors.Join(returnErr, file.Close())
		}()
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

	if file == nil {
		return nil
	}

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
