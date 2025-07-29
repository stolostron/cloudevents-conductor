package statusevent

import (
	"github.com/openshift-online/maestro/pkg/dao"
	"github.com/openshift-online/maestro/pkg/db"
	"github.com/openshift-online/maestro/pkg/services"
)

// NewStatusEventService creates a new StatusEventService with the provided session factory to interact with the database.
func NewStatusEventService(sessionFactory db.SessionFactory) services.StatusEventService {
	return services.NewStatusEventService(dao.NewStatusEventDao(&sessionFactory))
}
