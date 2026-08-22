package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/11DingKing/embodied-reading-studio/internal/domain/reading"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/review"
	"github.com/11DingKing/embodied-reading-studio/internal/domain/seminar"
	appmiddleware "github.com/11DingKing/embodied-reading-studio/internal/middleware"
	"github.com/11DingKing/embodied-reading-studio/internal/repository"
	"github.com/11DingKing/embodied-reading-studio/internal/service"
	"github.com/go-chi/chi/v5"
)

type HealthStore interface {
	Ping(context.Context) error
}

type API struct {
	Auth       service.AuthService
	Catalog    service.CatalogService
	Programs   service.ProgramService
	Readings   service.ReadingService
	Reviews    service.ReviewService
	Seminars   service.SeminarService
	Archives   service.ArchiveService
	Store      HealthStore
	Middleware appmiddleware.HTTP
	Logger     *slog.Logger
}

func (a API) Handler() http.Handler {
	router := chi.NewRouter()
	router.Use(a.Middleware.Recover)
	router.Use(a.Middleware.RequestID)
	router.Use(a.Middleware.Log)
	router.Get("/health/live", a.live)
	router.Get("/health/ready", a.ready)
	router.Post("/v1/login", a.login)
	router.Group(func(protected chi.Router) {
		protected.Use(a.Middleware.Authenticate)
		protected.Post("/v1/logout", a.logout)
		protected.Post("/v1/editions", a.registerEdition)
		protected.Post("/v1/programs", a.createProgram)
		protected.Post("/v1/programs/{programID}/open", a.openProgram)
		protected.Post("/v1/programs/{programID}/join", a.joinProgram)
		protected.Post("/v1/programs/{programID}/start", a.startProgram)
		protected.Post("/v1/programs/{programID}/review", a.requestReview)
		protected.Post("/v1/programs/{programID}/assignments", a.reserve)
		protected.Put("/v1/assignments/{assignmentID}", a.moveReservation)
		protected.Post("/v1/readings", a.startReading)
		protected.Post("/v1/readings/{readingID}/submit", a.submitReading)
		protected.Post("/v1/readings/{readingID}/abandon", a.abandonReading)
		protected.Post("/v1/claims/{claimID}/reviews", a.assignReview)
		protected.Post("/v1/reviews/{reviewID}/claim", a.claimReview)
		protected.Post("/v1/reviews/{reviewID}/decide", a.decideReview)
		protected.Post("/v1/claims/{claimID}/disputes", a.openDispute)
		protected.Get("/v1/reviews", a.reviewQueue)
		protected.Post("/v1/agendas/{agendaID}/start", a.startAgenda)
		protected.Post("/v1/agendas/{agendaID}/items/{itemID}", a.resolveAgendaItem)
		protected.Post("/v1/agendas/{agendaID}/close", a.closeAgenda)
		protected.Post("/v1/programs/{programID}/archive", a.requestArchive)
	})
	return router
}

func (a API) actor(request *http.Request) service.Actor {
	principal, _ := appmiddleware.PrincipalFrom(request.Context())
	return service.Actor{TenantID: principal.User.TenantID, UserID: principal.User.ID, RequestID: appmiddleware.RequestID(request.Context())}
}

func (a API) live(response http.ResponseWriter, request *http.Request) {
	writeJSON(response, http.StatusOK, map[string]string{"status": "alive"})
}

func (a API) ready(response http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
	defer cancel()
	if err := a.Store.Ping(ctx); err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"status": "ready"})
}

func (a API) login(response http.ResponseWriter, request *http.Request) {
	input, ok := decode[service.LoginInput](response, request)
	if !ok {
		return
	}
	result, err := a.Auth.Login(request.Context(), appmiddleware.RequestID(request.Context()), input)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (a API) logout(response http.ResponseWriter, request *http.Request) {
	token := strings.TrimSpace(strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer "))
	if err := a.Auth.Logout(request.Context(), a.actor(request), token); err != nil {
		writeError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (a API) registerEdition(response http.ResponseWriter, request *http.Request) {
	input, ok := decode[service.RegisterEditionInput](response, request)
	if !ok {
		return
	}
	result, err := a.Catalog.RegisterEdition(request.Context(), a.actor(request), input)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusCreated, result)
}

func (a API) createProgram(response http.ResponseWriter, request *http.Request) {
	input, ok := decode[service.CreateProgramInput](response, request)
	if !ok {
		return
	}
	result, err := a.Programs.Create(request.Context(), a.actor(request), input)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusCreated, result)
}

func (a API) openProgram(response http.ResponseWriter, request *http.Request) {
	result, err := a.Programs.OpenEnrollment(request.Context(), a.actor(request), chi.URLParam(request, "programID"))
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (a API) joinProgram(response http.ResponseWriter, request *http.Request) {
	key := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	requestHash := repository.HashRequest(request.Method, request.URL.Path, nil)
	result, err := a.Programs.Join(request.Context(), a.actor(request), chi.URLParam(request, "programID"), key, requestHash)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusCreated, result)
}

func (a API) startProgram(response http.ResponseWriter, request *http.Request) {
	result, err := a.Programs.Start(request.Context(), a.actor(request), chi.URLParam(request, "programID"))
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (a API) requestReview(response http.ResponseWriter, request *http.Request) {
	value, readiness, err := a.Programs.RequestReview(request.Context(), a.actor(request), chi.URLParam(request, "programID"))
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusAccepted, map[string]any{"program": value, "readiness": readiness})
}

func (a API) reserve(response http.ResponseWriter, request *http.Request) {
	input, ok := decode[service.ReserveInput](response, request)
	if !ok {
		return
	}
	input.ProgramID = chi.URLParam(request, "programID")
	result, err := a.Readings.Reserve(request.Context(), a.actor(request), input)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusCreated, result)
}

func (a API) moveReservation(response http.ResponseWriter, request *http.Request) {
	input, ok := decode[struct {
		Pages    reading.PageRange
		StartsAt time.Time
		EndsAt   time.Time
	}](response, request)
	if !ok {
		return
	}
	result, err := a.Readings.MoveReservation(request.Context(), a.actor(request), chi.URLParam(request, "assignmentID"), input.Pages, input.StartsAt, input.EndsAt)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (a API) startReading(response http.ResponseWriter, request *http.Request) {
	input, ok := decode[service.StartReadingInput](response, request)
	if !ok {
		return
	}
	result, err := a.Readings.Start(request.Context(), a.actor(request), input)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusCreated, result)
}

func (a API) submitReading(response http.ResponseWriter, request *http.Request) {
	input, ok := decode[service.SubmitReadingInput](response, request)
	if !ok {
		return
	}
	input.SessionID = chi.URLParam(request, "readingID")
	result, err := a.Readings.Submit(request.Context(), a.actor(request), input)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (a API) abandonReading(response http.ResponseWriter, request *http.Request) {
	input, ok := decode[struct{ Reason string }](response, request)
	if !ok {
		return
	}
	if err := a.Readings.Abandon(request.Context(), a.actor(request), chi.URLParam(request, "readingID"), input.Reason); err != nil {
		writeError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (a API) assignReview(response http.ResponseWriter, request *http.Request) {
	input, ok := decode[struct{ ReviewerID string }](response, request)
	if !ok {
		return
	}
	result, err := a.Reviews.Assign(request.Context(), a.actor(request), chi.URLParam(request, "claimID"), input.ReviewerID)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusCreated, result)
}

func (a API) claimReview(response http.ResponseWriter, request *http.Request) {
	input, ok := decode[struct{ WorkerID string }](response, request)
	if !ok {
		return
	}
	result, err := a.Reviews.Claim(request.Context(), a.actor(request), chi.URLParam(request, "reviewID"), input.WorkerID)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (a API) decideReview(response http.ResponseWriter, request *http.Request) {
	input, ok := decode[service.DecideInput](response, request)
	if !ok {
		return
	}
	input.AssignmentID = chi.URLParam(request, "reviewID")
	result, err := a.Reviews.Decide(request.Context(), a.actor(request), input)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (a API) openDispute(response http.ResponseWriter, request *http.Request) {
	input, ok := decode[struct{ Reason string }](response, request)
	if !ok {
		return
	}
	result, err := a.Reviews.OpenDispute(request.Context(), a.actor(request), chi.URLParam(request, "claimID"), input.Reason)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusCreated, result)
}

func (a API) reviewQueue(response http.ResponseWriter, request *http.Request) {
	limit, _ := strconv.Atoi(request.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(request.URL.Query().Get("offset"))
	result, err := a.Reviews.Queue(request.Context(), a.actor(request), []review.AssignmentState{review.Queued, review.Leased}, repository.Page{Limit: limit, Offset: offset})
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (a API) startAgenda(response http.ResponseWriter, request *http.Request) {
	result, err := a.Seminars.Start(request.Context(), a.actor(request), chi.URLParam(request, "agendaID"))
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (a API) resolveAgendaItem(response http.ResponseWriter, request *http.Request) {
	input, ok := decode[struct {
		State   seminar.ItemState
		Outcome string
	}](response, request)
	if !ok {
		return
	}
	result, err := a.Seminars.ResolveItem(request.Context(), a.actor(request), chi.URLParam(request, "agendaID"), chi.URLParam(request, "itemID"), input.State, input.Outcome)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (a API) closeAgenda(response http.ResponseWriter, request *http.Request) {
	result, err := a.Seminars.Close(request.Context(), a.actor(request), chi.URLParam(request, "agendaID"))
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (a API) requestArchive(response http.ResponseWriter, request *http.Request) {
	job, readiness, err := a.Archives.Request(request.Context(), a.actor(request), chi.URLParam(request, "programID"))
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusAccepted, map[string]any{"job": job, "readiness": readiness})
}
