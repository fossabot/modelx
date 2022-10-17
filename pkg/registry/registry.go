package registry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/opencontainers/go-digest"
	"kubegems.io/modelx/pkg/errors"
	"kubegems.io/modelx/pkg/types"
)

type Registry struct {
	Manifest *RegistryStore
}

func (s *Registry) HeadManifest(w http.ResponseWriter, r *http.Request) {
	name, reference := GetRepositoryReference(r)
	exist, err := s.Manifest.Exists(r.Context(), name, reference)
	if err != nil {
		ResponseError(w, err)
		return
	}
	if exist {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *Registry) GetGlobalIndex(w http.ResponseWriter, r *http.Request) {
	index, err := s.Manifest.GetGlobalIndex(r.Context(), r.URL.Query().Get("search"))
	if err != nil {
		if IsS3StorageNotFound(err) {
			ResponseOK(w, types.Index{})
		}
		ResponseError(w, err)
		return
	}
	ResponseOK(w, index)
}

func (s *Registry) GetIndex(w http.ResponseWriter, r *http.Request) {
	name, _ := GetRepositoryReference(r)
	index, err := s.Manifest.GetIndex(r.Context(), name, r.URL.Query().Get("search"))
	if err != nil {
		if IsS3StorageNotFound(err) {
			err = errors.NewIndexUnknownError(name)
		}
		ResponseError(w, err)
		return
	}
	ResponseOK(w, index)
}

func (s *Registry) DeleteIndex(w http.ResponseWriter, r *http.Request) {
	name, _ := GetRepositoryReference(r)
	if err := s.Manifest.RemoveIndex(r.Context(), name); err != nil {
		if IsS3StorageNotFound(err) {
			err = errors.NewIndexUnknownError(name)
		}
		ResponseError(w, err)
		return
	}
	ResponseOK(w, "ok")
}

func (s *Registry) GetManifest(w http.ResponseWriter, r *http.Request) {
	name, reference := GetRepositoryReference(r)
	manifest, err := s.Manifest.GetManifest(r.Context(), name, reference)
	if err != nil {
		ResponseError(w, err)
		return
	}
	ResponseOK(w, manifest)
}

func (s *Registry) PutManifest(w http.ResponseWriter, r *http.Request) {
	name, reference := GetRepositoryReference(r)
	var manifest types.Manifest
	if err := json.NewDecoder(r.Body).Decode(&manifest); err != nil {
		ResponseError(w, errors.NewManifestInvalidError(err))
		return
	}
	contenttype := r.Header.Get("Content-Type")
	if err := s.Manifest.PutManifest(r.Context(), name, reference, contenttype, manifest); err != nil {
		ResponseError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Registry) DeleteManifest(w http.ResponseWriter, r *http.Request) {
	name, reference := GetRepositoryReference(r)
	if err := s.Manifest.DeleteManifest(r.Context(), name, reference); err != nil {
		ResponseError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func GetRepositoryReference(r *http.Request) (string, string) {
	vars := mux.Vars(r)
	return vars["name"], vars["reference"]
}

func (s *Registry) HeadBlob(w http.ResponseWriter, r *http.Request) {
	BlobDigestFun(w, r, func(ctx context.Context, repository string, digest digest.Digest) {
		ok, err := s.Manifest.ExistsBlob(r.Context(), repository, digest)
		if err != nil {
			ResponseError(w, err)
			return
		}
		if ok {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

// 如果客户端 包含 contentLength 则直接上传
// 如果客户端 不包含 contentLength 则返回一个 Location 后续上传至该地址
func (s *Registry) PutBlob(w http.ResponseWriter, r *http.Request) {
	BlobDigestFun(w, r, func(ctx context.Context, repository string, digest digest.Digest) {
		contentType := r.Header.Get("Content-Type")
		if contentType == "" {
			ResponseError(w, errors.NewContentTypeInvalidError("empty"))
			return
		}
		content := StorageContent{
			ContentLength: r.ContentLength,
			ContentType:   contentType,
			Content:       r.Body,
		}
		result, err := s.Manifest.PutBlob(r.Context(), repository, digest, content)
		if err != nil {
			ResponseError(w, err)
			return
		}
		if location := result.RedirectLocation; location != "" {
			w.Header().Set("Location", location)
			w.WriteHeader(http.StatusTemporaryRedirect)
		} else {
			w.WriteHeader(http.StatusCreated)
		}
	})
}

func (s *Registry) GetBlob(w http.ResponseWriter, r *http.Request) {
	BlobDigestFun(w, r, func(ctx context.Context, repository string, digest digest.Digest) {
		result, err := s.Manifest.GetBlob(r.Context(), repository, digest)
		if err != nil {
			ResponseError(w, err)
			return
		}
		if location := result.RedirectLocation; location != "" {
			w.Header().Add("Location", location)
			w.WriteHeader(http.StatusFound)
		} else {
			w.Header().Set("Content-Length", strconv.Itoa(int(result.Content.ContentLength)))
			w.Header().Set("Content-Type", result.Content.ContentType)
			w.Header().Set("Content-Encoding", result.Content.ContentEncoding)
			w.WriteHeader(http.StatusOK)

			io.Copy(w, result.Content.Content)
		}
		return
	})
}

func BlobDigestFun(w http.ResponseWriter, r *http.Request, fun func(ctx context.Context, repository string, digest digest.Digest)) {
	name, _ := GetRepositoryReference(r)
	digeststr := mux.Vars(r)["digest"]
	digest, err := digest.Parse(digeststr)
	if err != nil {
		ResponseError(w, errors.NewDigestInvalidError(digeststr))
		return
	}
	fun(r.Context(), name, digest)
}

func ParseDescriptor(r *http.Request) (types.Descriptor, error) {
	digeststr := mux.Vars(r)["digest"]
	digest, err := digest.Parse(digeststr)
	if err != nil {
		return types.Descriptor{}, errors.NewDigestInvalidError(digeststr)
	}
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		return types.Descriptor{}, errors.NewContentTypeInvalidError("empty")
	}
	descriptor := types.Descriptor{
		Digest:    digest,
		MediaType: contentType,
	}
	return descriptor, nil
}

func ParseAndCheckContentRange(header http.Header) (int64, int64, error) {
	contentRange, contentLength := header.Get("Content-Range"), header.Get("Content-Length")
	ranges := strings.Split(contentRange, "-")
	if len(ranges) != 2 {
		return -1, -1, errors.NewContentRangeInvalidError("invalid format")
	}
	start, err := strconv.ParseInt(ranges[0], 10, 64)
	if err != nil {
		return -1, -1, errors.NewContentRangeInvalidError("invalid start")
	}
	end, err := strconv.ParseInt(ranges[1], 10, 64)
	if err != nil {
		return -1, -1, errors.NewContentRangeInvalidError("invalid end")
	}
	if start > end {
		return -1, -1, errors.NewContentRangeInvalidError("start > end")
	}
	contentLen, err := strconv.ParseInt(contentLength, 10, 64)
	if err != nil {
		return -1, -1, errors.NewContentRangeInvalidError("invalid content length")
	}
	if contentLen != (end-start)+1 {
		return -1, -1, errors.NewContentRangeInvalidError("content length != (end-start)+1")
	}
	return start, end, nil
}
