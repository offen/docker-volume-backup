package main

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func createArchive(inputFilePath, outputFilePath string) (err error) {
	inputFilePath = stripTrailingSlashes(inputFilePath)
	inputFilePath, outputFilePath, err = makeAbsolute(inputFilePath, outputFilePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outputFilePath), 0755); err != nil {
		return err
	}

	err = compress(inputFilePath, outputFilePath, filepath.Dir(inputFilePath))
	if err != nil {
		return err
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

func compress(inPath, outFilePath, subPath string) (err error) {
	file, err := os.Create(outFilePath)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.Remove(outFilePath)
		}
	}()

	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)

	var paths []string
	if err := filepath.WalkDir(inPath, func(path string, di fs.DirEntry, err error) error {
		paths = append(paths, path)
		return err
	}); err != nil {
		return err
	}
	for _, p := range paths {
		if err := writeTarGz(p, tarWriter); err != nil {
			return err
		}
	}

	err = tarWriter.Close()
	if err != nil {
		return err
	}

	err = gzipWriter.Close()
	if err != nil {
		return err
	}

	err = file.Close()
	if err != nil {
		return err
	}

	return nil
}

func writeTarGz(path string, tarWriter *tar.Writer) error {
	fileInfo, err := os.Lstat(path)
	if err != nil {
		return err
	}

	if fileInfo.Mode()&os.ModeSocket == os.ModeSocket {
		return nil
	}

	var link string
	if fileInfo.Mode()&os.ModeSymlink == os.ModeSymlink {
		var err error
		if link, err = os.Readlink(path); err != nil {
			return err
		}
	}

	header, err := tar.FileInfoHeader(fileInfo, link)
	if err != nil {
		return err
	}
	header.Name = path

	err = tarWriter.WriteHeader(header)
	if err != nil {
		return err
	}

	if !fileInfo.Mode().IsRegular() {
		return nil
	}

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(tarWriter, file)
	if err != nil {
		return err
	}

	return err
}
