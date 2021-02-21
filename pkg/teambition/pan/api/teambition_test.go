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

func TestCreateFolder(t *testing.T) {
	ctx := setup(t)
	_, err := fs.CreateFolder(ctx, "/")
	require.NoError(t, err)
	_, err = fs.CreateFolder(ctx, "/test3/test4")
	require.NoError(t, err)
}

func TestRename(t *testing.T) {
	ctx := setup(t)
	node, err := fs.Get(ctx, "/test3/test4", FolderKind)
	require.NoError(t, err)
	err = fs.Rename(ctx, node, "test5")
	require.NoError(t, err)
}

func TestMove(t *testing.T) {
	ctx := setup(t)
	node, err := fs.Get(ctx, "/test3/test5", FolderKind)
	require.NoError(t, err)
	newNode, err := fs.Get(ctx, "/", FolderKind)
	require.NoError(t, err)
	err = fs.Move(ctx, node, newNode)
	require.NoError(t, err)
}

func TestRemove(t *testing.T) {
	ctx := setup(t)
	node, err := fs.Get(ctx, "/test5", FolderKind)
	require.NoError(t, err)
	err = fs.Remove(ctx, node)
	require.NoError(t, err)
	node, err = fs.Get(ctx, "/test3", FolderKind)
	require.NoError(t, err)
	err = fs.Remove(ctx, node)
	require.NoError(t, err)
}

func TestOpen(t *testing.T) {
	ctx := setup(t)
	node, err := fs.Get(ctx, "/media/2.jpg", FileKind)
	require.NoError(t, err)
	fd, err := fs.Open(ctx, node, map[string]string{})
	require.NoError(t, err)
	data, err := ioutil.ReadAll(fd)
	require.NoError(t, err)
	fo, err := os.Create("output.jpg")
	require.NoError(t, err)
	fo.Write(data)
	require.NoError(t, fd.Close())
	require.NoError(t, fo.Close())
}

func TestCreateFile(t *testing.T) {
	ctx := setup(t)
	fd, err := os.Open("1.mp3")
	require.NoError(t, err)
	info, err := fd.Stat()
	require.NoError(t, err)
	_, err = fs.CreateFile(ctx, "/media/1.mp3", info.Size(), fd, true)
	require.NoError(t, err)
	defer fd.Close()
}

func TestIntegration1(t *testing.T) {
	ctx := setup(t)
	node, err := fs.CreateFolder(ctx, "/test3")
	require.NoError(t, err)
	err = fs.Remove(ctx, node)
	require.NoError(t, err)
}

func TestCopy(t *testing.T) {
	ctx := setup(t)
	node, err := fs.Get(ctx, "/media/2.jpg", FileKind)
	require.NoError(t, err)
	parent, err := fs.Get(ctx, "/", FolderKind)
	require.NoError(t, err)
	err = fs.Copy(ctx, node, parent)
	require.NoError(t, err)
}

func TestGet(t *testing.T) {
	ctx := setup(t)
	node, err := fs.Get(ctx, "/media/2.jpg", FileKind)
	require.NoError(t, err)
	fmt.Println(node)
	node, err = fs.Get(ctx, "/media/not-exist.jpg", FileKind)
	require.NoError(t, err)
}
