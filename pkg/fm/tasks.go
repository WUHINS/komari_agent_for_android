package fm

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"sync"
)

type Stream interface {
	Send(data []byte) error
	Recv() ([]byte, error)
}

type Task struct {
	stream  Stream
	printf  func(string, ...interface{})
	payload []byte
}

func NewFMClient(stream Stream, printFunc func(string, ...interface{})) *Task {
	return &Task{
		stream: stream,
		printf: printFunc,
	}
}

func (t *Task) DoTask(data []byte) {
	t.payload = data

	switch t.payload[0] {
	case 0:
		t.listDir()
	case 1:
		go t.download()
	case 2:
		t.upload()
	}
}

func (t *Task) listDir() {
	dir := string(t.payload[1:])
	var entries []fs.DirEntry
	var err error
	for {
		entries, err = os.ReadDir(dir)
		if err != nil {
			usr, err := user.Current()
			if err != nil {
				t.stream.Send(CreateErr(err))
				return
			}
			dir = usr.HomeDir + string(filepath.Separator)
			continue
		}
		break
	}
	var buffer bytes.Buffer
	td := Create(&buffer, dir)
	for _, e := range entries {
		newBin := AppendFileName(td, e.Name(), e.IsDir())
		td = newBin
	}
	t.stream.Send(td)
}

func (t *Task) download() {
	path := string(t.payload[1:])
	file, err := os.Open(path)
	if err != nil {
		t.printf("Error opening file: %s", err)
		t.stream.Send(CreateErr(err))
		return
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		t.printf("Error getting file info: %s", err)
		t.stream.Send(CreateErr(err))
		return
	}

	fileSize := fileInfo.Size()
	if fileSize <= 0 {
		t.stream.Send(CreateErr(errors.New("requested file is empty")))
		return
	}

	var header bytes.Buffer
	headerData := CreateFile(&header, uint64(fileSize))
	if err := t.stream.Send(headerData); err != nil {
		t.printf("Error sending file header: %s", err)
		t.stream.Send(CreateErr(err))
		return
	}

	for {
		bp := bufPool.Get().(*bp)
		n, err := file.Read(bp.buf)
		if err != nil {
			if err == io.EOF {
				bufPool.Put(bp)
				return
			}
			t.printf("Error reading file: %s", err)
			t.stream.Send(CreateErr(err))
			bufPool.Put(bp)
			return
		}

		if err := t.stream.Send(bp.buf[:n]); err != nil {
			t.printf("Error sending file chunk: %s", err)
			t.stream.Send(CreateErr(err))
			bufPool.Put(bp)
			return
		}
		bufPool.Put(bp)
	}
}

func (t *Task) upload() {
	if len(t.payload) < 9 {
		const err = "data is invalid"
		t.printf(err)
		t.stream.Send(CreateErr(errors.New(err)))
		return
	}

	fileSize := binary.BigEndian.Uint64(t.payload[1:9])
	path := string(t.payload[9:])

	file, err := os.Create(path)
	if err != nil {
		t.printf("Error creating file: %s", err)
		t.stream.Send(CreateErr(err))
		return
	}
	defer file.Close()

	totalReceived := uint64(0)

	t.printf("receiving file: %s, size: %d", file.Name(), fileSize)
	for totalReceived < fileSize {
		data, err := t.stream.Recv()
		if err != nil {
			t.printf("Error receiving data: %s", err)
			t.stream.Send(CreateErr(err))
			return
		}

		bytesWritten, err := file.Write(data)
		if err != nil {
			t.printf("Error writing to file: %s", err)
			t.stream.Send(CreateErr(err))
			return
		}

		totalReceived += uint64(bytesWritten)
	}
	t.printf("received file %s.", file.Name())
	t.stream.Send(completeIdentifier)
}

type bp struct {
	buf []byte
}

var bufPool = sync.Pool{
	New: func() any {
		return &bp{
			buf: make([]byte, 1024*1024),
		}
	},
}
