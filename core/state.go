package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// State — то, что прототип поставил в системе поверх исходной сети.
// Пишется в журнал ДО каждого изменения (план этапа 1, раздел 1, п. 4),
// читается при восстановлении: при повторном запуске (раздел 5), при
// команде cleanup, при штатном отключении (раздел 4).
type State struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`

	// Исходное состояние — снято ДО любых наших изменений.
	PhysicalInterface string   `json:"physical_interface"`
	PhysicalGateway   string   `json:"physical_gateway"`
	PrimaryService    string   `json:"primary_service"`
	OriginalDNS       []string `json:"original_dns"`

	// Что поставлено — восстановление снимает то и только то, что здесь
	// отмечено, и только если это ещё стоит (план, раздел 4, п. 1 и 3).
	UtunName      string `json:"utun_name,omitempty"`
	FullTunnelSet bool   `json:"full_tunnel_set"`
	TunnelDNSSet  bool   `json:"tunnel_dns_set"`

	// ExceptionTargets — ЗАПАСНОЙ путь от петли (план, раздел 3):
	// маршруты /32 через физический шлюз для адресов, которые Protector
	// движка не смог привязать к физическому интерфейсу. В обычном
	// случае (Protector отработал на все сокеты) список пуст.
	ExceptionTargets []string `json:"exception_targets,omitempty"`

	// DirectDNSSet — поставлен DNS прямых доменов белого режима (этап 3,
	// core.DirectRouter). Маршруты прямых доменов — в ExceptionTargets
	// выше, тем же путём, что исключения движка.
	DirectDNSSet bool `json:"direct_dns_set,omitempty"`

	// RUDirectRoutes — «RU-адреса напрямую» (core.RUDirectRouter): блоки,
	// поставленные маршрутом через физический шлюз. Пишется ОДНИМ commit
	// ДО пачки маршрутов (тысячи записей — не по одной); Restore снимает
	// их все по журналу, даже если реально встала только часть.
	RUDirectRoutes []string `json:"ru_direct_routes,omitempty"`

	// IPv6Service/IPv6Off — платформа выключила IPv6 на физической службе
	// на время сессии (утечка мимо туннеля: IPv4 полностью перекрывается
	// половинками 0.0.0.0/1+128.0.0.0/1, но с IPv6 у macOS отдельная от
	// статических маршрутов логика по RA/scope, и маршруты в utun не
	// гарантируют, что браузер не уйдёт по AAAA напрямую — найдено
	// владельцем 28.09, "страна не поддерживается" на claude.ai через
	// точку доступа телефона при поднятом туннеле). Restore включает
	// обратно, если это поле true. IPv6Service — имя службы для
	// networksetup (platform/darwin), не заполняется на платформах без
	// этой возможности.
	IPv6Service string `json:"ipv6_service,omitempty"`
	IPv6Off     bool   `json:"ipv6_off,omitempty"`
}

// Store читает и пишет журнал по фиксированному пути атомарно (через
// временный файл + fsync + rename — обрыв посреди записи не оставляет
// половинчатый файл) и с правами 0600.
//
// Save потокобезопасен (mu): живой прогон 24.09 поймал гонку двух
// параллельных Save одного и того же журнала (движок зовёт
// SocketProtector.ExcludeHost из нескольких горутин почти одновременно
// — пробы DoH к 1.1.1.1/8.8.8.8/9.9.9.9 параллельно, каждая своим
// commit) — оба процесса писали в один и тот же ВРЕМЕННЫЙ файл
// (Path+".tmp"), rename одного падал с «no such file or directory»,
// потому что второй уже переименовал tmp-файл первым. mu здесь чинит
// это на уровне файла для ЛЮБОГО источника параллельных Save, не
// только для того, что уже нашёлся (в дополнение к сериализации внутри
// SocketProtector.ExcludeHost, protector.go — та закрывает гонку между
// вызовами ExcludeHost друг с другом, эта — гонку на самом файле
// независимо от источника).
//
// NB: mu защищает ЗАПИСЬ ФАЙЛА, не поля самого *State — конкурентная
// мутация полей State разными горутинами (Platform.Apply и
// SocketProtector.ExcludeHost одновременно) сама по себе остаётся вне
// этой блокировки; в этом прогоне она не проявилась (Apply уже
// завершился к моменту параллельных ExcludeHost), но это открытый
// вопрос на будущее, не закрытый этим фиксом.
type Store struct {
	Path string

	mu sync.Mutex
}

// DefaultStore — путь журнала прототипа.
func DefaultStore() *Store {
	return &Store{Path: "/var/run/meridian/state.json"}
}

// Load возвращает nil, nil, если журнала нет (это НЕ ошибка — обычное
// состояние «сейчас не подключены»).
func (s *Store) Load() (*State, error) {
	data, err := os.ReadFile(s.Path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("чтение журнала %s: %w", s.Path, err)
	}
	// BOM: журнал, открытый и сохранённый в Блокноте (Windows, ручная
	// правка/сторож), начинается с EF BB BF — json.Unmarshal на нём падает
	// (нашёл кот 6). Сами мы пишем без BOM.
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("разбор журнала %s: %w", s.Path, err)
	}
	return &st, nil
}

func (s *Store) Save(st *State) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return fmt.Errorf("создание каталога журнала: %w", err)
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("сериализация журнала: %w", err)
	}

	tmp := s.Path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("создание временного журнала: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("запись временного журнала: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("fsync временного журнала: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("закрытие временного журнала: %w", err)
	}
	if err := os.Rename(tmp, s.Path); err != nil {
		return fmt.Errorf("переименование журнала: %w", err)
	}
	return nil
}

// Remove удаляет журнал; отсутствие файла — не ошибка (идемпотентно).
func (s *Store) Remove() error {
	if err := os.Remove(s.Path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("удаление журнала %s: %w", s.Path, err)
	}
	return nil
}
