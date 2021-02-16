package api

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

var fs Fs

func setup(t *testing.T) context.Context {
	cookie := ""
	cb, err := ioutil.ReadFile("../../../../.cookie")
	if err == nil {
		cookie = string(cb)
	}
	config := &Config{
		Cookie: cookie,
	}

	ctx := context.Background()
	fs, err = NewFs(ctx, config)
	require.NoError(t, err)
	return ctx
}

func TestList(t *testing.T) {
	ctx := setup(t)
	names, err := fs.List(ctx, "/long")
	require.NoError(t, err)
	println(fmt.Sprintf("size: %v, %v", len(names), names))
}

func TestOpen(t *testing.T) {
	ctx := setup(t)
	fd, err := fs.Open(ctx, "/media/2.jpg", map[string]string{})
	require.NoError(t, err)
	data, err := ioutil.ReadAll(fd)
	require.NoError(t, err)
	fo, err := os.Create("output.jpg")
	require.NoError(t, err)
	fo.Write(data)
	require.NoError(t, fd.Close())
	require.NoError(t, fo.Close())
}

func TestCreateFolder(t *testing.T) {
	ctx := setup(t)
	err := fs.CreateFolder(ctx, "/")
	require.NoError(t, err)
	err = fs.CreateFolder(ctx, "/test3/test4")
	require.NoError(t, err)
}

func TestRemove(t *testing.T) {
	ctx := setup(t)
	err := fs.Remove(ctx, "/test3/test4")
	require.NoError(t, err)
	err = fs.Remove(ctx, "/test3")
	require.NoError(t, err)
}

func TestCreateFile(t *testing.T) {
	ctx := setup(t)
	fd, err := os.Open("1.mp3")
	require.NoError(t, err)
	info, err := fd.Stat()
	require.NoError(t, err)
	err = fs.CreateFile(ctx, "/media/1.mp3", info.Size(), fd, true)
	require.NoError(t, err)
	defer fd.Close()
}

func TestRename(t *testing.T) {
	ctx := setup(t)
	err := fs.Rename(ctx, "/test", "test2")
	require.NoError(t, err)
}

func TestMove(t *testing.T) {
	ctx := setup(t)
	err := fs.Move(ctx, "/home/test2", "/")
	require.NoError(t, err)
}
