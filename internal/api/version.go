package api

import (
	"github.com/gofiber/fiber/v3"
)

type (
	// Version is what the binary reports about itself. It is read from the
	// build rather than configured: a version that can disagree with the
	// binary is worse than no version, because it is trusted.
	Version struct {
		Version string
		Commit  string
	}

	// VersionResponse is the payload of GET /version.
	VersionResponse struct {
		Component string `json:"component"`
		Version   string `json:"version"`
		Commit    string `json:"commit"`
	}
)

// NewVersionHandler returns a plain fiber handler exposing the component's
// build version. Registered outside the Huma API so it stays off the OpenAPI
// spec and untouched by the redirect event middleware.
func NewVersionHandler(component string, info *Version) fiber.Handler {
	resp := VersionResponse{Component: component, Version: "unknown", Commit: "unknown"}
	if info != nil {
		resp.Version = info.Version
		resp.Commit = info.Commit
	}

	return func(c fiber.Ctx) error {
		return c.JSON(resp)
	}
}
