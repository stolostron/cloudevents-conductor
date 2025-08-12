package consumer

import (
	"github.com/openshift-online/maestro/pkg/dao"
	"github.com/openshift-online/maestro/pkg/db"
	"github.com/openshift-online/maestro/pkg/services"
)

func NewConsumerService(sessionFactory db.SessionFactory) services.ConsumerService {
	return services.NewConsumerService(
		dao.NewConsumerDao(&sessionFactory),
	)
}
