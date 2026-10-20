package bamboohr

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
)

// File is one file stored against an employee or the company.
type File struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	// OriginalFileName is the name the file was uploaded under, including its
	// extension.
	OriginalFileName string `json:"originalFileName"`
	// Size is the file size in bytes. The company and employee endpoints
	// disagree on whether to send it as a string or a number, so it is decoded
	// leniently.
	Size FlexInt `json:"size"`
	// DateCreated is an ISO 8601 timestamp with a UTC offset.
	DateCreated string `json:"dateCreated"`
	// CreatedBy is the full name of the uploading user.
	CreatedBy string `json:"createdBy"`
	// ShareWithEmployee is "yes" when the file is shared with the employee it
	// belongs to.
	ShareWithEmployee string `json:"shareWithEmployee,omitempty"`
	// ShareWithEmployees is "yes" when a company file is shared with everyone.
	ShareWithEmployees string `json:"shareWithEmployees,omitempty"`
	CanRenameFile      string `json:"canRenameFile,omitempty"`
	CanDeleteFile      string `json:"canDeleteFile,omitempty"`
}

// Shared reports whether the file is shared with the employees who can see it.
func (f File) Shared() bool {
	return yesNo(f.ShareWithEmployee) || yesNo(f.ShareWithEmployees)
}

// FileCategory groups files as BambooHR displays them.
type FileCategory struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	// CanUploadFiles is "yes" when the API key's user may add files here.
	CanUploadFiles string `json:"canUploadFiles,omitempty"`
	Files          []File `json:"files"`
}

// EmployeeFiles is the file list for one employee.
type EmployeeFiles struct {
	Employee struct {
		ID int `json:"id"`
	} `json:"employee"`
	Categories []FileCategory `json:"categories"`
}

// All flattens the categories into a single slice, which is what a listing
// wants.
func (e EmployeeFiles) All() []File {
	var files []File
	for _, cat := range e.Categories {
		files = append(files, cat.Files...)
	}
	return files
}

// CompanyFiles is the company's file list.
type CompanyFiles struct {
	Categories []FileCategory `json:"categories"`
}

// All flattens the categories into a single slice.
func (c CompanyFiles) All() []File {
	var files []File
	for _, cat := range c.Categories {
		files = append(files, cat.Files...)
	}
	return files
}

// ListEmployeeFiles returns the files and categories for one employee.
func (c *Client) ListEmployeeFiles(ctx context.Context, employeeID string) (*EmployeeFiles, error) {
	if strings.TrimSpace(employeeID) == "" {
		return nil, fmt.Errorf("bamboohr: employee id is required")
	}
	var out EmployeeFiles
	path := "/api/v1/employees/" + url.PathEscape(employeeID) + "/files/view"
	// This endpoint defaults to XML, so the JSON Accept header the client
	// always sends is what selects the shape decoded here.
	if err := c.get(ctx, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DownloadEmployeeFile streams one of an employee's files into w.
func (c *Client) DownloadEmployeeFile(ctx context.Context, employeeID string, fileID int, w io.Writer) (*Download, error) {
	if strings.TrimSpace(employeeID) == "" {
		return nil, fmt.Errorf("bamboohr: employee id is required")
	}
	if fileID <= 0 {
		return nil, fmt.Errorf("bamboohr: file id is required")
	}
	path := "/api/v1/employees/" + url.PathEscape(employeeID) + "/files/" + strconv.Itoa(fileID)
	return c.download(ctx, path, nil, w)
}

// ListCompanyFiles returns the company files and categories visible to the API
// key's user.
func (c *Client) ListCompanyFiles(ctx context.Context) (*CompanyFiles, error) {
	var out CompanyFiles
	if err := c.get(ctx, "/api/v1/files/view", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DownloadCompanyFile streams a company file into w.
func (c *Client) DownloadCompanyFile(ctx context.Context, fileID int, w io.Writer) (*Download, error) {
	if fileID <= 0 {
		return nil, fmt.Errorf("bamboohr: file id is required")
	}
	return c.download(ctx, "/api/v1/files/"+strconv.Itoa(fileID), nil, w)
}
