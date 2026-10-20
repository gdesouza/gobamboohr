package bamboohr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListEmployeeFilesFlattensCategories(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want := "/api/v1/employees/12/files/view"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		w.Write([]byte(`{
			"employee": {"id": 12},
			"categories": [
				{"id":1,"name":"Signed Documents","canUploadFiles":"yes","files":[
					{"id":5,"name":"Offer","originalFileName":"offer.pdf","size":2048,
					 "dateCreated":"2024-01-15T12:30:00+0000","createdBy":"HR","shareWithEmployee":"yes"}
				]},
				{"id":2,"name":"Tax","files":[
					{"id":6,"name":"T4","originalFileName":"t4.pdf","size":1024,"shareWithEmployee":"no"}
				]}
			]
		}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	files, err := c.ListEmployeeFiles(context.Background(), "12")
	if err != nil {
		t.Fatalf("ListEmployeeFiles: %v", err)
	}
	if files.Employee.ID != 12 {
		t.Errorf("employee id = %d, want 12", files.Employee.ID)
	}
	all := files.All()
	if len(all) != 2 {
		t.Fatalf("All() returned %d files, want 2 across both categories", len(all))
	}
	if all[0].Size.Int64() != 2048 {
		t.Errorf("size = %d, want 2048", all[0].Size.Int64())
	}
	if !all[0].Shared() {
		t.Error("Shared() = false for shareWithEmployee=yes")
	}
	if all[1].Shared() {
		t.Error("Shared() = true for shareWithEmployee=no")
	}
}

func TestListCompanyFilesHandlesStringSize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// The company endpoint sends the size as a string, unlike the employee
		// one.
		w.Write([]byte(`{"categories":[{"id":1,"name":"Policies","files":[
			{"id":3,"name":"Handbook","originalFileName":"handbook.pdf","size":"51200",
			 "shareWithEmployees":"yes"}
		]}]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	files, err := c.ListCompanyFiles(context.Background())
	if err != nil {
		t.Fatalf("ListCompanyFiles: %v", err)
	}
	all := files.All()
	if len(all) != 1 {
		t.Fatalf("got %d files, want 1", len(all))
	}
	if all[0].Size.Int64() != 51200 {
		t.Errorf("size = %d, want 51200", all[0].Size.Int64())
	}
	if !all[0].Shared() {
		t.Error("Shared() = false for shareWithEmployees=yes")
	}
}

func TestDownloadEmployeeFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want := "/api/v1/employees/12/files/5"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		w.Header().Set("Content-Disposition", `attachment; filename="offer.pdf"`)
		w.Write([]byte("pdf-bytes"))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	var buf strings.Builder
	meta, err := c.DownloadEmployeeFile(context.Background(), "12", 5, &buf)
	if err != nil {
		t.Fatalf("DownloadEmployeeFile: %v", err)
	}
	if meta.Filename != "offer.pdf" {
		t.Errorf("Filename = %q, want offer.pdf", meta.Filename)
	}
	if buf.String() != "pdf-bytes" {
		t.Errorf("wrote %q, want pdf-bytes", buf.String())
	}
}

func TestFileDownloadsValidateArguments(t *testing.T) {
	c, err := New("acme", "key")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var sink strings.Builder
	if _, err := c.DownloadEmployeeFile(context.Background(), "", 1, &sink); err == nil {
		t.Error("blank employee id succeeded, want an error")
	}
	if _, err := c.DownloadEmployeeFile(context.Background(), "1", 0, &sink); err == nil {
		t.Error("zero file id succeeded, want an error")
	}
	if _, err := c.DownloadCompanyFile(context.Background(), 0, &sink); err == nil {
		t.Error("zero company file id succeeded, want an error")
	}
	if _, err := c.ListEmployeeFiles(context.Background(), ""); err == nil {
		t.Error("blank employee id succeeded, want an error")
	}
}
