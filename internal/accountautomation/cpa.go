package accountautomation

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"mime"
	"os"
	"path"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const (
	maxCPAResponseSize = 16 << 20
	maxCPAZIPFiles     = 100
	maxCPAZIPFileSize  = 2 << 20
	maxCPAZIPTotalSize = 8 << 20
)

func ParseCPA(download DownloadedCPA) ([]Credential, error) {
	if len(download.Data) == 0 {
		return nil, errors.New("CPA response is empty")
	}
	if len(download.Data) > maxCPAResponseSize {
		return nil, errors.New("CPA response exceeds size limit")
	}

	mediaType, _, err := mime.ParseMediaType(download.ContentType)
	if err != nil {
		return nil, errors.New("CPA content type is invalid")
	}
	switch strings.ToLower(mediaType) {
	case "application/json", "text/json":
		credential, err := parseCPACredential(download.Data)
		if err != nil {
			return nil, err
		}
		return []Credential{credential}, nil
	case "application/zip", "application/x-zip-compressed":
		return parseCPAZIP(download.Data)
	default:
		return nil, errors.New("CPA content type is unsupported")
	}
}

func parseCPAZIP(data []byte) ([]Credential, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("CPA ZIP is invalid")
	}
	if len(reader.File) == 0 {
		return nil, errors.New("CPA ZIP is empty")
	}
	if len(reader.File) > maxCPAZIPFiles {
		return nil, errors.New("CPA ZIP exceeds file count limit")
	}

	credentials := make([]Credential, 0, len(reader.File))
	var expanded uint64
	for _, file := range reader.File {
		if err := validateCPAZIPEntry(file); err != nil {
			return nil, err
		}
		if file.UncompressedSize64 > maxCPAZIPFileSize {
			return nil, errors.New("CPA ZIP entry exceeds size limit")
		}
		expanded += file.UncompressedSize64
		if expanded > maxCPAZIPTotalSize {
			return nil, errors.New("CPA ZIP exceeds expanded size limit")
		}

		body, err := readLimitedZIPEntry(file)
		if err != nil {
			return nil, err
		}
		if isArchive(body) {
			return nil, errors.New("CPA ZIP contains a nested archive")
		}
		credential, err := parseCPACredential(body)
		if err != nil {
			return nil, errors.New("CPA ZIP contains an invalid credential")
		}
		credentials = append(credentials, credential)
	}
	return credentials, nil
}

func validateCPAZIPEntry(file *zip.File) error {
	name := file.Name
	portableName := strings.ReplaceAll(name, `\`, "/")
	if name == "" || path.IsAbs(portableName) || isWindowsAbsolutePath(portableName) || strings.HasPrefix(portableName, "/") {
		return errors.New("CPA ZIP contains an absolute path")
	}
	for _, part := range strings.Split(portableName, "/") {
		if part == ".." {
			return errors.New("CPA ZIP contains path traversal")
		}
	}
	mode := file.Mode()
	if mode&os.ModeSymlink != 0 || !mode.IsRegular() {
		return errors.New("CPA ZIP contains a non-regular file")
	}
	if !strings.EqualFold(path.Ext(portableName), ".json") {
		return errors.New("CPA ZIP contains a non-JSON file")
	}
	return nil
}

func isWindowsAbsolutePath(name string) bool {
	return len(name) >= 3 && ((name[0] >= 'A' && name[0] <= 'Z') || (name[0] >= 'a' && name[0] <= 'z')) && name[1] == ':' && name[2] == '/'
}

func readLimitedZIPEntry(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, errors.New("CPA ZIP entry cannot be opened")
	}
	defer reader.Close()

	body, err := io.ReadAll(io.LimitReader(reader, maxCPAZIPFileSize+1))
	if err != nil {
		return nil, errors.New("CPA ZIP entry cannot be read")
	}
	if len(body) > maxCPAZIPFileSize {
		return nil, errors.New("CPA ZIP entry exceeds size limit")
	}
	return body, nil
}

func parseCPACredential(data []byte) (Credential, error) {
	var fields map[string]any
	if err := common.Unmarshal(data, &fields); err != nil || fields == nil {
		return Credential{}, errors.New("CPA credential JSON is invalid")
	}
	accessToken, ok := nonEmptyString(fields["access_token"])
	if !ok {
		return Credential{}, errors.New("CPA credential access_token must be a non-empty string")
	}
	accountID, ok := nonEmptyString(fields["account_id"])
	if !ok {
		return Credential{}, errors.New("CPA credential account_id must be a non-empty string")
	}

	canonical, err := common.Marshal(fields)
	if err != nil {
		return Credential{}, errors.New("CPA credential cannot be normalized")
	}
	var credential Credential
	if err := common.Unmarshal(canonical, &credential); err != nil {
		return Credential{}, errors.New("CPA credential fields have invalid types")
	}
	credential.AccessToken = accessToken
	credential.AccountID = accountID
	return credential, nil
}

func nonEmptyString(value any) (string, bool) {
	text, ok := value.(string)
	return text, ok && strings.TrimSpace(text) != ""
}

func isArchive(data []byte) bool {
	return len(data) >= 4 && (bytes.Equal(data[:4], []byte("PK\x03\x04")) ||
		bytes.Equal(data[:4], []byte("PK\x05\x06")) ||
		bytes.Equal(data[:4], []byte("PK\x07\x08")))
}
