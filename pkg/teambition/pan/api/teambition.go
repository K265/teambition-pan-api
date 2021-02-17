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
	Get(ctx context.Context, path string, kind string) (*Node, error)
	List(ctx context.Context, path string) ([]Node, error)
	CreateFolder(ctx context.Context, path string) (*Node, error)
	Rename(ctx context.Context, node *Node, newName string) error
	Move(ctx context.Context, node *Node, newPath string) error
	Remove(ctx context.Context, node *Node) error
	Open(ctx context.Context, node *Node, headers map[string]string) (io.ReadCloser, error)
	CreateFile(ctx context.Context, path string, size int64, in io.Reader, overwrite bool) (*Node, error)
}

type Config struct {
	Cookie string
}

func (config Config) String() string {
	return fmt.Sprintf("Config{Cookie: %s}", config.Cookie)
}

type Teambition struct {
	folderCache Cache
	config      Config
	orgId       string
	memberId    string
	rootId      string
	rootNode    Node
	driveId     string
	ApiBaseUrl  string
	httpClient  *http.Client
	mutex       sync.Mutex
}

func (teambition *Teambition) String() string {
	return fmt.Sprintf("Teambition{orgId: %s, memberId: %s}", teambition.orgId, teambition.memberId)
}

func (teambition *Teambition) request(ctx context.Context, method, url string, headers map[string]string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	res, err2 := teambition.httpClient.Do(req)
	if err2 != nil {
		return nil, errors.WithStack(err2)
	}
	return res, nil
}

func (teambition *Teambition) jsonRequest(ctx context.Context, method, url string, body io.Reader, model interface{}) error {
	headers := map[string]string{
		"Content-Type": "application/json",
		"Cookie":       teambition.config.Cookie,
	}
	res, err := teambition.request(ctx, method, url, headers, body)
	if err != nil {
		return errors.WithStack(err)
	}
	defer res.Body.Close()

	if model != nil {
		b, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return errors.Wrap(err, `error reading res.Body`)
		}
		err = json.Unmarshal(b, &model)
		if err != nil {
			return errors.Wrapf(err, "error parsing model, response: %s", string(b))
		}
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
		config:      *config,
		ApiBaseUrl:  BaseUrl,
		httpClient:  client,
		folderCache: cache,
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

const FolderKind = "folder"
const FileKind = "file"
const AnyKind = "any"

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

func (teambition *Teambition) Get(ctx context.Context, path string, kind string) (*Node, error) {
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

	nodeId, ok := teambition.folderCache.Get(parent)
	if !ok {
		node, err := teambition.Get(ctx, parent, FolderKind)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		nodeId = node.NodeId
		teambition.folderCache.Put(parent, nodeId)
	}

	return teambition.findNameNode(ctx, &Node{NodeId: nodeId}, name, kind)
}

func findNodeError(err error, path string) error {
	return errors.Wrapf(err, `error finding node of "%s"`, path)
}

func marshalError(err error) error {
	return errors.Wrap(err, "error marshalling body")
}

func (teambition *Teambition) List(ctx context.Context, path string) ([]Node, error) {
	path = normalizePath(path)
	node, err := teambition.Get(ctx, path, FolderKind)
	if err != nil {
		return nil, findNodeError(err, path)
	}

	nodes, err2 := teambition.listNodes(ctx, node)
	if err2 != nil {
		return nil, errors.Wrapf(err2, `error listing nodes of "%s"`, node)
	}

	return nodes.Data, nil
}

func (teambition *Teambition) createFolderInternal(ctx context.Context, parent string, name string) (*Node, error) {
	teambition.mutex.Lock()
	defer teambition.mutex.Unlock()

	node, err := teambition.Get(ctx, parent+"/"+name, FolderKind)
	if err == nil {
		return node, nil
	}

	node, err = teambition.Get(ctx, parent, FolderKind)
	if err != nil {
		return nil, findNodeError(err, parent)
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
		return nil, marshalError(err)
	}
	var createdNode Node
	err = teambition.jsonRequest(ctx, "POST", "https://pan.teambition.com/pan/api/nodes/folder", bytes.NewBuffer(b), createdNode)
	if err != nil {
		return nil, errors.Wrap(err, "error posting create folder request")
	}
	return &createdNode, nil
}

func (teambition *Teambition) CreateFolder(ctx context.Context, path string) (*Node, error) {
	path = normalizePath(path)
	pathLen := len(path)
	i := 0
	var createdNode *Node
	for i < pathLen {
		parent := path[:i]
		remain := path[i+1:]
		j := strings.Index(remain, "/")
		if j < 0 {
			// already at last position
			return teambition.createFolderInternal(ctx, parent, remain)
		} else {
			node, err := teambition.createFolderInternal(ctx, parent, remain[:j])
			if err != nil {
				return nil, err
			}
			i = i + j + 1
			createdNode = node
		}
	}

	return createdNode, nil
}

func (teambition *Teambition) checkRoot(node *Node) error {
	if node == nil {
		return errors.New("empty node")
	}
	if node.NodeId == teambition.rootId {
		return errors.New("can't operate on root ")
	}
	return nil
}

func (teambition *Teambition) Rename(ctx context.Context, node *Node, newName string) error {
	if err := teambition.checkRoot(node); err != nil {
		return err
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
	err = teambition.jsonRequest(ctx, "PUT", fmt.Sprintf("https://pan.teambition.com/pan/api/nodes/%s", node.NodeId), bytes.NewBuffer(b), nil)
	if err != nil {
		return errors.Wrap(err, `error posting rename request`)
	}
	return nil
}

func (teambition *Teambition) Move(ctx context.Context, node *Node, newPath string) error {
	if err := teambition.checkRoot(node); err != nil {
		return err
	}

	newNode, err := teambition.Get(ctx, newPath, AnyKind)
	if err != nil {
		return findNodeError(err, newPath)
	}
	body := map[string]interface{}{
		"orgId":     teambition.orgId,
		"driveId":   teambition.driveId,
		"sameLevel": false,
		"ids": []map[string]string{
			{
				"id":        node.NodeId,
				"ccpFileId": node.NodeId,
			},
		},
		"parentId": newNode.NodeId,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return marshalError(err)
	}
	err = teambition.jsonRequest(ctx, "POST", "https://pan.teambition.com/pan/api/nodes/move", bytes.NewBuffer(b), nil)
	if err != nil {
		return errors.Wrap(err, `error posting move request`)
	}
	return nil
}

func (teambition *Teambition) Remove(ctx context.Context, node *Node) error {
	if err := teambition.checkRoot(node); err != nil {
		return err
	}

	body := map[string]interface{}{
		"nodeIds": []string{node.NodeId},
		"orgId":   teambition.orgId,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return marshalError(err)
	}
	err = teambition.jsonRequest(ctx, "POST", "https://pan.teambition.com/pan/api/nodes/archive", bytes.NewBuffer(b), nil)
	if err != nil {
		return errors.Wrap(err, `error posting remove request`)
	}
	return nil
}

func (teambition *Teambition) getByNode(ctx context.Context, node *Node) (*Node, error) {
	var detail Node
	err := teambition.jsonRequest(ctx, "GET", fmt.Sprintf("https://pan.teambition.com/pan/api/nodes/%s?orgId=%s&driveId=%s", node.NodeId, teambition.orgId, teambition.driveId), nil, &detail)
	if err != nil {
		return nil, errors.Wrapf(err, "error getting node detail, node: %s", node)
	}
	return &detail, nil
}

func (teambition *Teambition) Open(ctx context.Context, node *Node, headers map[string]string) (io.ReadCloser, error) {
	if err := teambition.checkRoot(node); err != nil {
		return nil, err
	}

	detail, err := teambition.getByNode(ctx, node)
	if err != nil {
		return nil, err
	}

	downloadUrl := detail.DownloadUrl
	if downloadUrl == "" {
		return nil, errors.Errorf(`error getting downloadUrl of "%s"`, node)
	}

	res, err := teambition.request(ctx, "GET", downloadUrl, headers, nil)
	if err != nil {
		return nil, errors.Wrapf(err, `error downloading "%s"`, downloadUrl)
	}

	return res.Body, nil
}

func (teambition *Teambition) CreateFile(ctx context.Context, path string, size int64, in io.Reader, overwrite bool) (*Node, error) {
	path = normalizePath(path)
	i := strings.LastIndex(path, "/")
	parent := path[:i]
	name := path[i+1:]
	_, err := teambition.CreateFolder(ctx, parent)
	if err != nil {
		return nil, errors.New("error creating folder")
	}

	node, err := teambition.Get(ctx, parent, FolderKind)
	if err != nil {
		return nil, findNodeError(err, parent)
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
		return nil, err
	}

	uploadName := uploadResults[0].Name
	if name != uploadName && overwrite {
		node, err := teambition.Get(ctx, parent+"/"+name, FileKind)
		if err == nil {
			err = teambition.Remove(ctx, node)
			if err == nil {
				err = preUpload()
				if err != nil {
					return nil, err
				}
			}
		}
	}

	uploadUrl := uploadResults[0].UploadUrl[0]
	{
		req, err := http.NewRequestWithContext(ctx, "PUT", uploadUrl, in)
		if err != nil {
			return nil, errors.Wrap(err, "error creating upload request")
		}
		req.Header.Set("Content-Length", strconv.FormatInt(size, 10))
		req.Header.Set("Content-Type", "")
		ursp, err := teambition.httpClient.Do(req)
		if err != nil {
			return nil, errors.Wrap(err, "error uploading file")
		}
		defer ursp.Body.Close()
	}

	var createdNode Node
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
			return nil, marshalError(err)
		}

		err = teambition.jsonRequest(ctx, "POST", "https://pan.teambition.com/pan/api/nodes/complete", bytes.NewBuffer(b), &createdNode)
		if err != nil {
			return nil, errors.Wrap(err, `error posting upload complete request`)
		}
	}
	return &createdNode, nil
}
