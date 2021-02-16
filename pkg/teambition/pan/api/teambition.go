package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	errors "github.com/pkg/errors"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

var BaseUrl = "https://pan.teambition.com"

type Fs interface {
	CreateFolder(ctx context.Context, path string) error
	GetNode(ctx context.Context, path string, kind string) (*Node, error)
	List(ctx context.Context, path string) (names []Node, err error)
	Open(ctx context.Context, path string) (io.ReadCloser, error)
	Remove(ctx context.Context, path string) error
	CreateFile(ctx context.Context, path string, size int64, in io.Reader, overwrite bool) error
	Rename(ctx context.Context, path string, newName string) error
	Move(ctx context.Context, oldPath string, newPath string) error
}

type Config struct {
	Cookie string
}

func (config Config) String() string {
	return fmt.Sprintf("Config{Cookie: %s}", config.Cookie)
}

type Teambition struct {
	pathNodeCache Cache
	config        Config
	orgId         string
	memberId      string
	rootId        string
	rootNode      Node
	driveId       string
	ApiBaseUrl    string
	httpClient    *http.Client
	mutex         sync.Mutex
}

func (teambition *Teambition) String() string {
	return fmt.Sprintf("Teambition{orgId: %s, memberId: %s}", teambition.orgId, teambition.memberId)
}

func (teambition *Teambition) request(ctx context.Context, method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", teambition.config.Cookie)
	res, err2 := teambition.httpClient.Do(req)
	if err2 != nil {
		return nil, errors.WithStack(err2)
	}
	return res, nil
}

func (teambition *Teambition) jsonRequest(ctx context.Context, method, url string, body io.Reader, model interface{}) error {
	res, err := teambition.request(ctx, method, url, body)
	if err != nil {
		return errors.WithStack(err)
	}
	defer res.Body.Close()

	b, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return errors.Wrap(err, `error reading res.Body`)
	}
	err = json.Unmarshal(b, &model)
	if err != nil {
		return errors.Wrapf(err, "error parsing model, response: %s", string(b))
	}
	return nil
}

func NewFs(ctx context.Context, config *Config) (Fs, error) {
	cache, cerr := NewCache(256)
	if cerr != nil {
		return nil, errors.Wrap(cerr, "error creating cache")
	}

	client := &http.Client{}
	teambition := &Teambition{
		config:        *config,
		ApiBaseUrl:    BaseUrl,
		httpClient:    client,
		pathNodeCache: cache,
	}

	// get orgId, memberId
	{
		var personal Personal
		err := teambition.jsonRequest(ctx, "GET", "https://www.teambition.com/api/organizations/personal", nil, &personal)
		if err != nil {
			return nil, errors.Wrap(err, "error getting orgId, memberId")
		}

		teambition.orgId = personal.Id
		teambition.memberId = personal.CreatorId
	}

	// get root parentId
	{
		var spaces []Space
		err := teambition.jsonRequest(ctx, "GET", fmt.Sprintf("https://pan.teambition.com/pan/api/spaces?orgId=%s&memberId=%s", teambition.orgId, teambition.memberId), nil, &spaces)
		if err != nil {
			return nil, errors.Wrap(err, "error getting root parentId")
		}
		if len(spaces) < 1 {
			return nil, errors.New("empty spaces")
		}
		teambition.rootId = spaces[0].RootId
		n := &Node{
			NodeId: teambition.rootId,
			Kind:   "folder",
			Name:   "Root",
		}
		teambition.rootNode = *n
	}

	// get driveId
	{
		var drive Drive
		err := teambition.jsonRequest(ctx, "GET", fmt.Sprintf("https://pan.teambition.com/pan/api/orgs/%s?orgId=%s", teambition.orgId, teambition.orgId), nil, &drive)
		if err != nil {
			return nil, errors.Wrap(err, "error getting driveId")
		}
		teambition.driveId = drive.Data.DriveId
	}

	return teambition, nil
}

// https://pan.teambition.com/pan/api/nodes?orgId=&driveId=&parentId=
func (teambition *Teambition) listNodes(ctx context.Context, node *Node) (*Nodes, error) {
	format := "https://pan.teambition.com/pan/api/nodes?limit=10000&orderBy=name&orderDirection=asc&orgId=%s&driveId=%s&parentId=%s"
	var nodes Nodes
	err := teambition.jsonRequest(ctx, "GET", fmt.Sprintf(format, teambition.orgId, teambition.driveId, node.NodeId), nil, &nodes)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return &nodes, nil
}

func (teambition *Teambition) findNameNode(ctx context.Context, node *Node, name string, kind string) (*Node, error) {
	nodes, err := teambition.listNodes(ctx, node)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	for _, d := range nodes.Data {
		if d.Name == name && (kind == AnyKind || d.Kind == kind) {
			return &d, nil
		}
	}

	return nil, errors.Errorf(`can't find "%s" under "%s"`, name, node)
}

func (teambition *Teambition) GetNode(ctx context.Context, path string, kind string) (*Node, error) {
	path = normalizePath(path)

	if path == "/" || path == "" {
		return &teambition.rootNode, nil
	}

	i := strings.LastIndex(path, "/")
	parent := path[:i]
	name := path[i+1:]
	if i == 0 {
		return teambition.findNameNode(ctx, &teambition.rootNode, name, kind)
	}

	nodeId, ok := teambition.pathNodeCache.Get(parent)
	if !ok {
		node, err := teambition.GetNode(ctx, parent, FolderKind)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		nodeId = node.NodeId
		teambition.pathNodeCache.Put(parent, nodeId)
	}

	return teambition.findNameNode(ctx, &Node{NodeId: nodeId}, name, kind)
}

func findNodeError(err error, path string) error {
	return errors.Wrapf(err, `error finding node of "%s"`, path)
}

func marshalError(err error) error {
	return errors.Wrap(err, "error marshalling body")
}

const FolderKind = "folder"
const FileKind = "file"
const AnyKind = "any"

func (teambition *Teambition) List(ctx context.Context, path string) ([]Node, error) {
	path = normalizePath(path)
	node, err := teambition.GetNode(ctx, path, FolderKind)
	if err != nil {
		return nil, findNodeError(err, path)
	}

	nodes, err2 := teambition.listNodes(ctx, node)
	if err2 != nil {
		return nil, errors.Wrapf(err2, `error listing nodes of "%s"`, node)
	}

	return nodes.Data, nil
}

func (teambition *Teambition) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	node, err := teambition.GetNode(ctx, path, FileKind)
	if err != nil {
		return nil, findNodeError(err, path)
	}

	downloadUrl := node.DownloadUrl
	if downloadUrl == "" {
		return nil, errors.Errorf(`error getting downloadUrl of "%s"`, node)
	}

	res, err := teambition.request(ctx, "GET", downloadUrl, nil)
	if err != nil {
		return nil, errors.Wrapf(err, `error downloading "%s"`, downloadUrl)
	}

	return res.Body, nil
}

func (teambition *Teambition) createFolderInternal(ctx context.Context, parent string, name string) error {
	teambition.mutex.Lock()
	defer teambition.mutex.Unlock()

	_, err := teambition.GetNode(ctx, parent+"/"+name, FolderKind)
	if err == nil {
		return nil
	}

	node, err := teambition.GetNode(ctx, parent, FolderKind)
	if err != nil {
		return findNodeError(err, parent)
	}
	body := map[string]string{
		"ccpParentId":   node.NodeId,
		"checkNameMode": "refuse",
		"driveId":       teambition.driveId,
		"name":          name,
		"orgId":         teambition.orgId,
		"parentId":      node.NodeId,
		"spaceId":       teambition.rootId,
		"type":          "folder",
	}
	b, err := json.Marshal(body)
	if err != nil {
		return marshalError(err)
	}
	res, err := teambition.request(ctx, "POST", "https://pan.teambition.com/pan/api/nodes/folder", bytes.NewBuffer(b))
	if err != nil {
		return errors.Wrap(err, "error posting create folder request")
	}
	defer res.Body.Close()
	return nil
}

// path must start with "/" and must not end with "/"
func normalizePath(s string) string {
	separator := "/"
	if !strings.HasPrefix(s, separator) {
		s = separator + s
	}

	if len(s) > 1 && strings.HasSuffix(s, separator) {
		s = s[:len(s)-1]
	}
	return s
}

func (teambition *Teambition) CreateFolder(ctx context.Context, path string) error {
	path = normalizePath(path)
	pathLen := len(path)
	i := 0
	for i < pathLen {
		parent := path[:i]
		remain := path[i+1:]
		j := strings.Index(remain, "/")
		if j < 0 {
			// already at last position
			return teambition.createFolderInternal(ctx, parent, remain)
		} else {
			err := teambition.createFolderInternal(ctx, parent, remain[:j])
			if err != nil {
				return err
			}
			i = i + j + 1
		}
	}

	return nil
}

func (teambition *Teambition) CreateFile(ctx context.Context, path string, size int64, in io.Reader, overwrite bool) error {
	path = normalizePath(path)
	i := strings.LastIndex(path, "/")
	parent := path[:i]
	name := path[i+1:]
	err := teambition.CreateFolder(ctx, parent)
	if err != nil {
		return errors.New("error creating folder")
	}

	node, err := teambition.GetNode(ctx, parent, FolderKind)
	if err != nil {
		return findNodeError(err, parent)
	}

	var uploadResults []UploadResult

	preUpload := func() error {
		body := map[string]interface{}{
			"orgId":         teambition.orgId,
			"spaceId":       teambition.rootId,
			"parentId":      node.NodeId,
			"checkNameMode": "autoRename",
			"infos": []map[string]interface{}{
				{
					"name":        name,
					"ccpParentId": node.NodeId,
					"driveId":     teambition.driveId,
					"size":        size,
					"chunkCount":  1,
					"contentType": "",
					"type":        "file",
				},
			},
		}
		b, err := json.Marshal(body)
		if err != nil {
			return marshalError(err)
		}

		err = teambition.jsonRequest(ctx, "POST", "https://pan.teambition.com/pan/api/nodes/file", bytes.NewBuffer(b), &uploadResults)
		if err != nil {
			return errors.Wrap(err, `error posting create file request`)
		}

		if len(uploadResults) < 1 || len(uploadResults[0].UploadUrl) < 1 {
			return errors.New(`error extracting uploadUrl'`)
		}

		return nil
	}

	err = preUpload()
	if err != nil {
		return err
	}

	uploadName := uploadResults[0].Name
	if name != uploadName && overwrite {
		err = teambition.Remove(ctx, parent+"/"+name)
		if err == nil {
			err = preUpload()
			if err != nil {
				return err
			}
		}

	}

	uploadUrl := uploadResults[0].UploadUrl[0]
	{
		req, err := http.NewRequestWithContext(ctx, "PUT", uploadUrl, in)
		if err != nil {
			return errors.Wrap(err, "error creating upload request")
		}
		req.Header.Set("Content-Length", strconv.FormatInt(size, 10))
		req.Header.Set("Content-Type", "")
		ursp, err := teambition.httpClient.Do(req)
		if err != nil {
			return errors.Wrap(err, "error uploading file")
		}
		defer ursp.Body.Close()
	}

	{
		body := map[string]interface{}{
			"driveId":   teambition.driveId,
			"orgId":     teambition.orgId,
			"nodeId":    uploadResults[0].NodeId,
			"uploadId":  uploadResults[0].UploadId,
			"ccpFileId": uploadResults[0].NodeId,
		}
		b, err := json.Marshal(body)
		if err != nil {
			return marshalError(err)
		}
		res, err := teambition.request(ctx, "POST", "https://pan.teambition.com/pan/api/nodes/complete", bytes.NewBuffer(b))
		if err != nil {
			return errors.Wrap(err, `error posting upload complete request`)
		}
		defer res.Body.Close()
	}
	return nil
}

func checkRoot(path string) error {
	if path == "" || path == "/" {
		return errors.New("can't operate on root ")
	}
	return nil
}

func (teambition *Teambition) Remove(ctx context.Context, path string) error {
	if err := checkRoot(path); err != nil {
		return err
	}

	node, err := teambition.GetNode(ctx, path, AnyKind)
	if err != nil {
		return findNodeError(err, path)
	}
	body := map[string]interface{}{
		"nodeIds": []string{node.NodeId},
		"orgId":   teambition.orgId,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return marshalError(err)
	}
	res, err := teambition.request(ctx, "POST", "https://pan.teambition.com/pan/api/nodes/archive", bytes.NewBuffer(b))
	if err != nil {
		return errors.Wrap(err, `error posting remove request`)
	}
	defer res.Body.Close()
	return nil
}

func (teambition *Teambition) Rename(ctx context.Context, path string, newName string) error {
	if err := checkRoot(path); err != nil {
		return err
	}

	node, err := teambition.GetNode(ctx, path, AnyKind)
	if err != nil {
		return findNodeError(err, path)
	}
	body := map[string]interface{}{
		"orgId":     teambition.orgId,
		"driveId":   teambition.driveId,
		"ccpFileId": node.NodeId,
		"name":      newName,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return marshalError(err)
	}
	res, err := teambition.request(ctx, "PUT", fmt.Sprintf("https://pan.teambition.com/pan/api/nodes/%s", node.NodeId), bytes.NewBuffer(b))
	if err != nil {
		return errors.Wrap(err, `error posting rename request`)
	}
	defer res.Body.Close()
	return nil
}

func (teambition *Teambition) Move(ctx context.Context, oldPath string, newPath string) error {
	if err := checkRoot(oldPath); err != nil {
		return err
	}

	oldNode, err := teambition.GetNode(ctx, oldPath, AnyKind)
	if err != nil {
		return findNodeError(err, oldPath)
	}
	newNode, err := teambition.GetNode(ctx, newPath, AnyKind)
	if err != nil {
		return findNodeError(err, newPath)
	}
	body := map[string]interface{}{
		"orgId":     teambition.orgId,
		"driveId":   teambition.driveId,
		"sameLevel": false,
		"ids": []map[string]string{
			{
				"id":        oldNode.NodeId,
				"ccpFileId": oldNode.NodeId,
			},
		},
		"parentId": newNode.NodeId,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return marshalError(err)
	}
	res, err := teambition.request(ctx, "POST", "https://pan.teambition.com/pan/api/nodes/move", bytes.NewBuffer(b))
	if err != nil {
		return errors.Wrap(err, `error posting move request`)
	}
	defer res.Body.Close()
	return nil
}
