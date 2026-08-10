package httpapi

import (
	"os"
	"path/filepath"
)

type resources struct {
	Root      string
	DB        string
	Avatars   string
	QR        string
	Templates string
	Static    string
}

func ensureResources(root string) (resources, error) {
	res := resources{
		Root:      root,
		DB:        filepath.Join(root, "db"),
		Avatars:   filepath.Join(root, "avatars"),
		QR:        filepath.Join(root, "qr"),
		Templates: filepath.Join(root, "templates"),
		Static:    filepath.Join(root, "static"),
	}
	for _, p := range []string{res.DB, res.Avatars, res.QR, res.Templates, filepath.Join(res.Static, "css"), filepath.Join(res.Static, "js")} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return res, err
		}
	}
	return res, nil
}

func (r resources) avatarPath(openid string) string {
	return filepath.Join(r.Avatars, safeName(openid)+".jpg")
}

func (r resources) qrPath(sessionID string) string {
	return filepath.Join(r.QR, sessionID+".jpg")
}
