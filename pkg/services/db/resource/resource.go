package resource

import (
	"github.com/openshift-online/maestro/pkg/services"

	"github.com/openshift-online/maestro/pkg/dao"
	"github.com/openshift-online/maestro/pkg/db"
)

// NewResourceService creates a new ResourceService with the provided session factory to interact with the database.
func NewResourceService(sessionFactory db.SessionFactory) services.ResourceService {
	return services.NewResourceService(
		db.NewAdvisoryLockFactory(sessionFactory),
		dao.NewResourceDao(&sessionFactory),
		services.NewEventService(dao.NewEventDao(&sessionFactory)),
		services.NewGenericService(dao.NewGenericDao(&sessionFactory)),
	)
}
