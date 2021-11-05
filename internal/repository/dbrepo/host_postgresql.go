package dbrepo

import (
	"context"
	"log"
	"time"

	"github.com/brianmaksy/go-watch/internal/models"
)

// InsertHost inserts a host into the database
func (m *postgresDBRepo) InsertHost(h models.Host) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel() // NTS - only cancel when function finishes (in the case the above is successful and ctx has value). Otherwise, cancel() will run anyway.

	query := `insert into hosts (host_name, canonical_name, url, ip, ipv6, location, os, active, created_at, updated_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) returning id`

	var newID int

	// QueryRowContext returns at most one SQLrow or value specified by query, then Scan puts the value into param
	err := m.DB.QueryRowContext(ctx, query,
		h.HostName,
		h.CanonicalName,
		h.URL,
		h.IP,
		h.IPV6,
		h.Location,
		h.OS,
		h.Active,
		time.Now(),
		time.Now(),
	).Scan(&newID)

	if err != nil {
		log.Println(err)
		return newID, err
	}

	// add host services and set to inactive. Service ID = 1 [NTS - for now].
	stmt := `
		insert into host_services (host_id, service_id, active, schedule_number, schedule_unit,
		created_at, updated_at, status) values ($1, 1, 0, 3, 'm', $2, $3, 'pending')
		`

	_, err = m.DB.ExecContext(ctx, stmt, newID, time.Now(), time.Now())
	if err != nil {
		return newID, err
	}
	return newID, nil

}

func (m *postgresDBRepo) GetHostByID(id int) (models.Host, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	query := `
		select id, host_name, canonical_name, url, ip, ipv6, location, os, active, created_at, updated_at
		from hosts where id = $1
	`

	row := m.DB.QueryRowContext(ctx, query, id)

	var h models.Host

	// put DB values into each item in h.
	err := row.Scan(
		&h.ID,
		&h.HostName,
		&h.CanonicalName,
		&h.URL,
		&h.IP,
		&h.IPV6,
		&h.Location,
		&h.OS,
		&h.Active,
		&h.CreatedAt,
		&h.UpdatedAt,
	)
	if err != nil {
		return h, err
	}

	// get all services for host
	query = `
		select 
			hs.id, hs.host_id, hs.service_id, hs.active, hs.schedule_number, hs.schedule_unit, 
			hs.last_check, hs.status, hs.created_at, hs.updated_at,
			s.id, s.service_name, s.active, s.icon, s.created_at, s.updated_at
		from 
			host_services hs 
			left join services s on (s.id = hs.service_id)
		where 
			host_id = $1
			`

	rows, err := m.DB.QueryContext(ctx, query, h.ID)

	if err != nil {
		log.Println(err)
		return h, err
	}
	defer rows.Close()

	var hostServices []models.HostService
	for rows.Next() {
		var hs models.HostService
		err := rows.Scan(
			&hs.ID,
			&hs.HostID,
			&hs.ServiceID,
			&hs.Active,
			&hs.ScheduleNumber,
			&hs.ScheduleUnit,
			&hs.LastCheck,
			&hs.Status,
			&hs.CreatedAt,
			&hs.UpdatedAt,
			&hs.Service.ID,
			&hs.Service.ServiceName,
			&hs.Service.Active,
			&hs.Service.Icon,
			&hs.Service.CreatedAt,
			&hs.Service.UpdatedAt,
		)
		if err != nil {
			log.Println(err)
			return h, err
		}

		hostServices = append(hostServices, hs)
	}
	h.HostServices = hostServices
	return h, nil
}

func (m *postgresDBRepo) UpdateHost(h models.Host) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	stmt := `
		update hosts set host_name = $1, canonical_name = $2, url = $3, ip = $4, ipv6 = $5, 
		os = $6, location = $7, active = $8, updated_at = $9 where id = $10
		
		`
	_, err := m.DB.ExecContext(ctx, stmt,
		h.HostName,
		h.CanonicalName,
		h.URL,
		h.IP,
		h.IPV6,
		h.Location,
		h.OS,
		h.Active,
		time.Now(),
		h.ID,
	)
	if err != nil {
		log.Println(err)
		return err
	}
	return nil
}

// GetAllServiceStatusCounts get status count of ACTIVE services.
func (m *postgresDBRepo) GetAllServiceStatusCounts() (int, int, int, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	query := `
		select 
		(select count(id) from host_services where active = 1 and status = 'pending') as pending, 
		(select count(id) from host_services where active = 1 and status = 'healthy') as healthy, 
		(select count(id) from host_services where active = 1 and status = 'warning') as warning, 
		(select count(id) from host_services where active = 1 and status = 'problem') as problem
	`

	var pending, healthy, warning, problem int
	row := m.DB.QueryRowContext(ctx, query)
	err := row.Scan(
		&pending,
		&healthy,
		&warning,
		&problem,
	)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return pending, healthy, warning, problem, nil
}

// NTS - can probably get away with just calling GetHostsByID n times (or maybe not),
// but better to use a new function anyway to return type slice.
func (m *postgresDBRepo) AllHosts() ([]models.Host, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	query := `
		select id, host_name, canonical_name, url, ip, ipv6, location, os, 
		active, created_at, updated_at from hosts order by host_name
		`

	// NTS - not queryROWcontext because here returning more than one row.
	rows, err := m.DB.QueryContext(ctx, query)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	defer rows.Close()

	var hosts []models.Host
	for rows.Next() {
		var h models.Host
		err = rows.Scan(
			&h.ID,
			&h.HostName,
			&h.CanonicalName,
			&h.URL,
			&h.IP,
			&h.IPV6,
			&h.Location,
			&h.OS,
			&h.Active,
			&h.CreatedAt,
			&h.UpdatedAt,
		)
		if err != nil {
			log.Println(err)
			return nil, err
		}

		serviceQuery := `
		select 
			hs.id, hs.host_id, hs.service_id, hs.active, hs.schedule_number, hs.schedule_unit, 
			hs.last_check, hs.status, hs.created_at, hs.updated_at,
			s.id, s.service_name, s.active, s.icon, s.created_at, s.updated_at
		from 
			host_services hs 
			left join services s on (s.id = hs.service_id)
		where 
			host_id = $1
			`
		serviceRows, err := m.DB.QueryContext(ctx, serviceQuery, h.ID)
		if err != nil {
			log.Println(err)
			return nil, err
		}
		var hostServices []models.HostService
		for serviceRows.Next() {
			var hs models.HostService
			serviceRows.Scan(
				&hs.ID,
				&hs.HostID,
				&hs.ServiceID,
				&hs.Active,
				&hs.ScheduleNumber,
				&hs.ScheduleUnit,
				&hs.LastCheck,
				&hs.Status,
				&hs.CreatedAt,
				&hs.UpdatedAt,
				&hs.Service.ID,
				&hs.Service.ServiceName,
				&hs.Service.Active,
				&hs.Service.Icon,
				&hs.Service.CreatedAt,
				&hs.Service.UpdatedAt,
			)
			if err != nil {
				log.Println(err)
				return nil, err
			}
			hostServices = append(hostServices, hs)
			serviceRows.Close() // nts - not defer before a for loop
		}
		h.HostServices = hostServices
		hosts = append(hosts, h)
	}
	// return any error during iteration
	if err = rows.Err(); err != nil {
		log.Println(err)
		return nil, err
	}
	return hosts, nil
}

// UpdateHostServiceStatus updates the active status of a host service
func (m *postgresDBRepo) UpdateHostServiceStatus(hostID, serviceID, active int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// nts - order doesn't matter
	stmt := `
		update host_services set active = $1 where host_id = $2 and service_id = $3
	`

	_, err := m.DB.ExecContext(ctx, stmt, active, hostID, serviceID)
	if err != nil {
		return err
	}
	return nil
}

func (m *postgresDBRepo) GetServicesByStatus(status string) ([]models.HostService, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	query := `
		select 
			hs.id, hs.host_id, hs.service_id, hs.active, hs.schedule_number, hs.schedule_unit, 
			hs.last_check, hs.status, hs.created_at, hs.updated_at,
			h.host_name, s.service_name 
		from 
			host_services hs 
			left join hosts h on (hs.host_id = h.id)
			left join services s on (hs.service_id = s.id)
		where 
			status = $1
			and hs.active = 1
			order by host_name, service_name`

	var services []models.HostService

	serviceRows, err := m.DB.QueryContext(ctx, query, status)
	if err != nil {
		return services, err
	}
	defer serviceRows.Close()

	for serviceRows.Next() {
		var hs models.HostService
		err := serviceRows.Scan(
			&hs.ID,
			&hs.HostID,
			&hs.ServiceID,
			&hs.Active,
			&hs.ScheduleNumber,
			&hs.ScheduleUnit,
			&hs.LastCheck,
			&hs.Status,
			&hs.CreatedAt,
			&hs.UpdatedAt,
			&hs.HostName,
			&hs.Service.ServiceName,
		)
		if err != nil {
			return nil, err
		}
		services = append(services, hs)
		// before, I had this one extra line about services close(), that's why only one entry.
	}
	return services, nil
}

func (m *postgresDBRepo) GetHostServiceByID(id int) (models.HostService, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	query := `
		select 
			hs.id, hs.host_id, hs.service_id, hs.active, hs.schedule_number, hs.schedule_unit, 
			hs.last_check, hs.status, hs.created_at, hs.updated_at,
			s.id, s.service_name, s.active, s.icon, s.created_at, s.updated_at
		from 
			host_services hs 
			left join services s on (s.id = hs.service_id)
		where 
			host_id = $1
		`

	var hs models.HostService

	row := m.DB.QueryRowContext(ctx, query, id)

	err := row.Scan(
		&hs.ID,
		&hs.HostID,
		&hs.ServiceID,
		&hs.Active,
		&hs.ScheduleNumber,
		&hs.ScheduleUnit,
		&hs.LastCheck,
		&hs.Status,
		&hs.CreatedAt,
		&hs.UpdatedAt,
		&hs.Service.ID,
		&hs.Service.ServiceName,
		&hs.Service.Active,
		&hs.Service.Icon,
		&hs.Service.CreatedAt,
		&hs.Service.UpdatedAt,
	)
	if err != nil {
		log.Println(err)
		return hs, err
	}
	return hs, nil
}

// UpdateHostService updates a host service in the database
func (m *postgresDBRepo) UpdateHostService(hs models.HostService) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// nts - order doesn't matter
	stmt := `
		update host_services set 
			 host_id = $1, service_id = $2, active = $3, 
			 schedule_number = $4, schedule_unit = $5,
			 last_check = $6, status = $7, updated_at = $8 
		where 
			id = $9
	`

	_, err := m.DB.ExecContext(ctx, stmt,
		hs.HostID,
		hs.ServiceID,
		hs.Active,
		hs.ScheduleNumber,
		hs.ScheduleUnit,
		hs.LastCheck,
		hs.Status,
		hs.UpdatedAt,
		hs.ID,
	)
	if err != nil {
		return err
	}
	return nil
}

func (m *postgresDBRepo) GetServicesToMonitor() ([]models.HostService, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	query :=
		`select 
		hs.id, hs.host_id, hs.service_id, hs.active, hs.schedule_number, hs.schedule_unit, 
		hs.last_check, hs.status, hs.created_at, hs.updated_at,
		s.id, s.service_name, s.active, s.icon, s.created_at, s.updated_at,
		h.host_name
	from 
		host_services hs 
		left join services s on (s.id = hs.service_id)
		left join hosts h on (hs.host_id = h.id)
	where 
		h.active = 1 and hs.active = 1
	`

	var services []models.HostService

	rows, err := m.DB.QueryContext(ctx, query)
	if err != nil {
		log.Println(err)
	}
	defer rows.Close()

	for rows.Next() {
		var hs models.HostService
		err := rows.Scan(
			&hs.ID,
			&hs.HostID,
			&hs.ServiceID,
			&hs.Active,
			&hs.ScheduleNumber,
			&hs.ScheduleUnit,
			&hs.LastCheck,
			&hs.Status,
			&hs.CreatedAt,
			&hs.UpdatedAt,
			&hs.Service.ID,
			&hs.Service.ServiceName,
			&hs.Service.Active,
			&hs.Service.Icon,
			&hs.Service.CreatedAt,
			&hs.Service.UpdatedAt,
			&hs.HostName,
		)
		if err != nil {
			log.Println(err)
			return services, err
		}
		services = append(services, hs)
	}
	return services, nil
}