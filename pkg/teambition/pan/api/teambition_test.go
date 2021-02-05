package api

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/require"
)

var fs Fs
var err error

func setup(t *testing.T) context.Context {
	err := godotenv.Load("../../../../.env")
	config := &Config{
		TeambitionSessionId:    os.Getenv("TEAMBITION_SESSIONID"),
		TeambitionSessionIdSig: os.Getenv("TEAMBITION_SESSIONID_SIG"),
	}

	ctx := context.Background()
	fs, err = NewFs(ctx, config)
	require.NoError(t, err)
	return ctx
}

func TestList(t *testing.T) {
	ctx := setup(t)
	names, err := fs.List(ctx, "/media")
	require.NoError(t, err)
	println(fmt.Sprintf("%v", names))
}

func TestOpen(t *testing.T) {
	ctx := setup(t)
	fd, err := fs.Open(ctx, "/media/music/1.mp3")
	require.NoError(t, err)
	data, err := ioutil.ReadAll(fd)
	require.NoError(t, err)
	fo, err := os.Create("output.mp3")
	fo.Write(data)
	require.NoError(t, err)
	require.NoError(t, fd.Close())
	require.NoError(t, fo.Close())
}

func TestCreateFolder(t *testing.T) {
	ctx := setup(t)
	err := fs.CreateFolder(ctx, "/", "test2")
	require.NoError(t, err)
}

func TestRemove(t *testing.T) {
	ctx := setup(t)
	err := fs.Remove(ctx, "/test2")
	require.NoError(t, err)
}

func TestCreateFile(t *testing.T) {
	ctx := setup(t)
	fd, err := os.Open("1.mp3")
	require.NoError(t, err)
	info, err := fd.Stat()
	require.NoError(t, err)
	err = fs.CreateFile(ctx, "/media", "1.mp3", info.Size(), fd)
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
