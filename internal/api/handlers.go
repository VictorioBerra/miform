package api

import (
	"errors"
	"html"
	"net/http"
	"strings"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"miform/internal/parser"
	"miform/internal/service"
)

// Handler contains HTTP handlers for form submissions and success responses.
type Handler struct {
	service *service.SubmissionService
}

// NewHandler constructs a new API Handler.
func NewHandler(svc *service.SubmissionService) *Handler {
	return &Handler{service: svc}
}

// Submit handles both POST /submit and POST /submit/{id}.
func (h *Handler) Submit(e *core.RequestEvent) error {
	parsed, err := parser.ParseRequest(e.Request)
	if err != nil {
		if wantsJSON(e.Request) {
			return e.JSON(http.StatusBadRequest, map[string]any{
				"success": false,
				"message": "Failed to parse form request: " + err.Error(),
			})
		}
		return renderErrorHTML(e, http.StatusBadRequest, "Invalid Form Submission", err.Error())
	}

	formIdentifier := e.Request.PathValue("id")
	if formIdentifier == "" {
		formIdentifier = parsed.Controls.AccessKey
	}

	result, err := h.service.Process(e.Request.Context(), formIdentifier, parsed)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if errors.Is(err, service.ErrFormNotFound) {
			statusCode = http.StatusNotFound
		} else if errors.Is(err, service.ErrMissingAccessKey) {
			statusCode = http.StatusBadRequest
		} else if errors.Is(err, service.ErrDomainNotAllowed) {
			statusCode = http.StatusForbidden
		} else if errors.Is(err, service.ErrFormInactive) {
			statusCode = http.StatusBadRequest
		} else if errors.Is(err, service.ErrTurnstileMissing) || errors.Is(err, service.ErrTurnstileFailed) || errors.Is(err, service.ErrTurnstileConfig) {
			statusCode = http.StatusBadRequest
		} else if errors.Is(err, service.ErrSpamHTMLLinks) {
			statusCode = http.StatusBadRequest
		}

		if parsed.IsAJAX {
			return e.JSON(statusCode, map[string]any{
				"success": false,
				"message": err.Error(),
			})
		}
		return renderErrorHTML(e, statusCode, "Submission Failed", err.Error())
	}

	// Successful submission handling
	if parsed.IsAJAX {
		// Strict rule: never return HTTP 301/303 redirect to AJAX / Fetch callers
		return e.JSON(http.StatusOK, map[string]any{
			"success":  true,
			"message":  result.Message,
			"redirect": result.RedirectURL,
			"data":     result.Data,
		})
	}

	// Browser HTML form POST: redirect via HTTP 303 See Other
	return e.Redirect(http.StatusSeeOther, result.RedirectURL)
}

// Success renders the default confirmation page for browser form POSTs.
func (h *Handler) Success(e *core.RequestEvent) error {
	htmlContent := `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Submission Received - Thank You</title>
  <style>
    body {
      margin: 0;
      padding: 40px 16px;
      background-color: #f8fafc;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
      color: #0f172a;
      display: flex;
      justify-content: center;
      align-items: center;
      min-height: 80vh;
      box-sizing: border-box;
    }
    .card {
      background: #ffffff;
      border: 1px solid #e2e8f0;
      border-radius: 12px;
      padding: 40px 32px;
      max-width: 480px;
      width: 100%;
      text-align: center;
      box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.05), 0 2px 4px -2px rgba(0, 0, 0, 0.05);
    }
    .icon {
      width: 56px;
      height: 56px;
      background-color: #dcfce7;
      color: #16a34a;
      border-radius: 50%;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      margin-bottom: 20px;
    }
    h1 {
      font-size: 24px;
      font-weight: 700;
      margin: 0 0 12px;
      color: #0f172a;
    }
    p {
      color: #64748b;
      font-size: 15px;
      line-height: 1.6;
      margin: 0 0 28px;
    }
    .btn {
      display: inline-block;
      background-color: #0284c7;
      color: #ffffff;
      text-decoration: none;
      font-weight: 600;
      font-size: 14px;
      padding: 12px 24px;
      border-radius: 6px;
      transition: background-color 0.15s ease;
    }
    .btn:hover {
      background-color: #0369a1;
    }
  </style>
</head>
<body>
  <div class="card">
    <div class="icon">
      <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
        <path d="M20 6L9 17l-5-5"/>
      </svg>
    </div>
    <h1>Thank you!</h1>
    <p>Your message has been received successfully. We appreciate you reaching out and will get back to you shortly.</p>
    <a href="javascript:history.back()" class="btn">Go Back</a>
  </div>
  <script>
    window.addEventListener('pageshow', function(event) {
      if (event.persisted) {
        window.location.reload();
      }
    });
  </script>
</body>
</html>`
	return e.HTML(http.StatusOK, htmlContent)
}

func wantsJSON(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	contentType := r.Header.Get("Content-Type")
	return strings.Contains(accept, "application/json") || strings.Contains(contentType, "application/json")
}

func renderErrorHTML(e *core.RequestEvent, status int, title, message string) error {
	tmpl := `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{ .Title }}</title>
  <style>
    body {
      margin: 0;
      padding: 40px 16px;
      background-color: #f8fafc;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
      color: #0f172a;
      display: flex;
      justify-content: center;
      align-items: center;
      min-height: 80vh;
      box-sizing: border-box;
    }
    .card {
      background: #ffffff;
      border: 1px solid #fee2e2;
      border-radius: 12px;
      padding: 40px 32px;
      max-width: 480px;
      width: 100%;
      text-align: center;
      box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.05);
    }
    .icon {
      width: 56px;
      height: 56px;
      background-color: #fee2e2;
      color: #dc2626;
      border-radius: 50%;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      margin-bottom: 20px;
    }
    h1 {
      font-size: 22px;
      font-weight: 700;
      margin: 0 0 12px;
      color: #991b1b;
    }
    p {
      color: #4b5563;
      font-size: 15px;
      line-height: 1.6;
      margin: 0 0 28px;
    }
    .btn {
      display: inline-block;
      background-color: #475569;
      color: #ffffff;
      text-decoration: none;
      font-weight: 600;
      font-size: 14px;
      padding: 12px 24px;
      border-radius: 6px;
    }
    .btn:hover {
      background-color: #334155;
    }
  </style>
</head>
<body>
  <div class="card">
    <div class="icon">
      <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="12" cy="12" r="10"/>
        <line x1="12" y1="8" x2="12" y2="12"/>
        <line x1="12" y1="16" x2="12.01" y2="16"/>
      </svg>
    </div>
    <h1>{{ .Title }}</h1>
    <p>{{ .Message }}</p>
    <a href="javascript:history.back()" class="btn">Go Back</a>
  </div>
</body>
</html>`

	htmlOut := strings.ReplaceAll(tmpl, "{{ .Title }}", html.EscapeString(title))
	htmlOut = strings.ReplaceAll(htmlOut, "{{ .Message }}", html.EscapeString(message))

	return e.HTML(status, htmlOut)
}
