// Copyright 2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

// Portions of this file are taken from package `targz`, Copyright (c) 2014 Fredrik Wallgren
// Licensed under the MIT License: https://github.com/walle/targz/blob/57fe4206da5abf7dd3901b4af3891ec2f08c7b08/LICENSE

package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func createArchive(inputFilePath, outputFilePath string) error {
	inputFilePath = stripTrailingSlashes(inputFilePath)
	inputFilePath, outputFilePath, err := makeAbsolute(inputFilePath, outputFilePath)
	if err != nil {
		return fmt.Errorf("createArchive: error transposing given file paths: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outputFilePath), 0755); err != nil {
		return fmt.Errorf("createArchive: error creating output file path: %w", err)
	}

	if err := compress(inputFilePath, outputFilePath, filepath.Dir(inputFilePath)); err != nil {
		return fmt.Errorf("createArchive: error creating archive: %w", err)
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

func compress(inPath, outFilePath, subPath string) error {
	file, err := os.Create(outFilePath)
	if err != nil {
		return fmt.Errorf("compress: error creating out file: %w", err)
	}

	prefix := path.Dir(outFilePath)
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)

	var paths []string
	if err := filepath.WalkDir(inPath, func(path string, di fs.DirEntry, err error) error {
		paths = append(paths, path)
		return err
	}); err != nil {
		return fmt.Errorf("compress: error walking filesystem tree: %w", err)
	}

	for _, p := range paths {
		if err := writeTarGz(p, tarWriter, prefix); err != nil {
			return fmt.Errorf("compress error writing %s to archive: %w", p, err)
		}
	}

	err = tarWriter.Close()
	if err != nil {
		return fmt.Errorf("compress: error closing tar writer: %w", err)
	}

	err = gzipWriter.Close()
	if err != nil {
		return fmt.Errorf("compress: error closing gzip writer: %w", err)
	}

	err = file.Close()
	if err != nil {
		return fmt.Errorf("compress: error closing file: %w", err)
	}

	return nil
}

func writeTarGz(path string, tarWriter *tar.Writer, prefix string) error {
	fileInfo, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("writeTarGz: error getting file infor for %s: %w", path, err)
	}

	if fileInfo.Mode()&os.ModeSocket == os.ModeSocket {
		return nil
	}

	var link string
	if fileInfo.Mode()&os.ModeSymlink == os.ModeSymlink {
		var err error
		if link, err = os.Readlink(path); err != nil {
			return fmt.Errorf("writeTarGz: error resolving symlink %s: %w", path, err)
		}
	}

	header, err := tar.FileInfoHeader(fileInfo, link)
	if err != nil {
		return fmt.Errorf("writeTarGz: error getting file info header: %w", err)
	}
	header.Name = strings.TrimPrefix(path, prefix)

	err = tarWriter.WriteHeader(header)
	if err != nil {
		return fmt.Errorf("writeTarGz: error writing file info header: %w", err)
	}

	if !fileInfo.Mode().IsRegular() {
		return nil
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("writeTarGz: error opening %s: %w", path, err)
	}
	defer file.Close()

	_, err = io.Copy(tarWriter, file)
	if err != nil {
		return fmt.Errorf("writeTarGz: error copying %s to tar writer: %w", path, err)
	}

	return nil
}
