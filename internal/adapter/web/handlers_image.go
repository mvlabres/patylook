package web

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

const (
	// maxUploadBody bounds a whole upload request: the main image plus every carousel image.
	maxUploadBody = (1+domain.MaxExtraImages)*domain.MaxImageBytes + 1<<20
	// multipartMemory is how much of a multipart body is kept in memory before spilling to disk.
	multipartMemory = 8 << 20
)

// limitBody caps the request body size before anything reads it.
func limitBody(max int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > max {
			http.Error(w, "Arquivos muito grandes", http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, max)
		next.ServeHTTP(w, r)
	})
}

// readUploads collects the image files of a parsed multipart form. Empty file parts
// (a file input left blank) are ignored.
func readUploads(r *http.Request) (port.ProductImages, error) {
	var imgs port.ProductImages
	if r.MultipartForm == nil {
		return imgs, nil
	}
	read := func(h *multipart.FileHeader) (*port.ImageUpload, error) {
		if h.Filename == "" || h.Size == 0 {
			return nil, nil
		}
		if h.Size > domain.MaxImageBytes {
			return nil, fmt.Errorf("%s: imagem acima do limite de %d MB", h.Filename, domain.MaxImageBytes>>20)
		}
		f, err := h.Open()
		if err != nil {
			return nil, err
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, domain.MaxImageBytes+1))
		if err != nil {
			return nil, err
		}
		return &port.ImageUpload{Filename: h.Filename, Data: data}, nil
	}

	v := domain.NewValidationError()
	if hs := r.MultipartForm.File["main_image"]; len(hs) > 0 {
		up, err := read(hs[0])
		if err != nil {
			v.Add("main_image", err.Error())
		}
		imgs.Main = up
	}
	for _, h := range r.MultipartForm.File["images"] {
		up, err := read(h)
		if err != nil {
			v.Add("images", err.Error())
			continue
		}
		if up != nil {
			imgs.Extra = append(imgs.Extra, *up)
		}
	}
	return imgs, v.OrNil()
}

// serveImage streams a stored product image. Names are validated by the storage adapter,
// so only files the application wrote can be reached.
func (s *Server) serveImage(w http.ResponseWriter, r *http.Request) {
	f, modTime, err := s.svc.Catalog.OpenImage(r.Context(), r.PathValue("folder"), r.PathValue("file"))
	if err != nil {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, no-cache") // revalidated through Last-Modified; main image can be replaced
	http.ServeContent(w, r, r.PathValue("file"), modTime, f)
}

func (s *Server) showcase(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	page, err := s.svc.Catalog.Showcase(r.Context(), pageParam(r), q)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := s.newPage(r, "Mostruário", "showcase")
	p.Data["Items"] = page.Items
	p.Data["Query"] = q
	p.Data["Pager"] = pagerFor(r, page)
	s.view(w, r, "showcase", p)
}

// imageUpload adds carousel images (and optionally replaces the main one) of an existing product.
func (s *Server) imageUpload(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	imgs, err := readUploads(r)
	if err == nil {
		err = s.svc.Catalog.AddImages(r.Context(), principalFrom(r.Context()).User, id, imgs)
	}
	if fields, ok := fieldErrors(err); ok {
		s.productEditPage(w, r, http.StatusUnprocessableEntity, id, fields)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.success(w, r, "/products/"+strconv.Itoa(id)+"/edit", "Imagens enviadas.")
}

func (s *Server) imageDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	imageID, ok2 := strconv.Atoi(r.PathValue("imageId"))
	if !ok || ok2 != nil {
		s.fail(w, r, domain.ErrNotFound)
		return
	}
	dest := "/products/" + strconv.Itoa(id) + "/edit"
	if err := s.svc.Catalog.DeleteImage(r.Context(), id, imageID); err != nil {
		s.actionError(w, r, dest, err)
		return
	}
	s.success(w, r, dest, "Imagem removida.")
}
