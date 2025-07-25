package event

import (
	"github.com/openshift-online/maestro/pkg/dao"
	"github.com/openshift-online/maestro/pkg/db"
	"github.com/openshift-online/maestro/pkg/services"
)

// NewEventService creates a new EventService with the provided session factory to interacte with the database
func NewEventService(sessionFactory db.SessionFactory) services.EventService {
	return services.NewEventService(dao.NewEventDao(&sessionFactory))
}
