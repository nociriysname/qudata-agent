//go:build linux

package security

import (
	"fmt"
	"log"
	_ "os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// FanotifyMonitor отслеживает доступ к файлу и разрешает его только для определенного PID.
type FanotifyMonitor struct {
	fd           int
	watchPath    string
	allowedPID   int
	stopChan     chan struct{}
	stoppedChan  chan struct{}
	lockdownDeps lockdownDependencies
}

// NewFanotifyMonitor создает новый монитор fanotify.
func NewFanotifyMonitor(path string, pid int, deps lockdownDependencies) (*FanotifyMonitor, error) {
	// 1. Инициализируем fanotify.
	// FAN_CLASS_CONTENT - для получения событий до того, как процесс получит доступ к данным.
	// FAN_UNLIMITED_QUEUE/MARKS - снимаем стандартные ограничения на размер очереди.
	fd, err := unix.FanotifyInit(unix.FAN_CLASS_CONTENT|unix.FAN_CLOEXEC, unix.O_RDONLY)
	if err != nil {
		return nil, fmt.Errorf("FanotifyInit failed: %w", err)
	}

	// 2. Устанавливаем "метку" на файл, который хотим отслеживать.
	// FAN_OPEN_PERM - мы хотим перехватывать события запроса на открытие файла.
	// FAN_MARK_ADD - добавляем новую метку.
	err = unix.FanotifyMark(fd, unix.FAN_MARK_ADD, unix.FAN_OPEN_PERM, -1, path)
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("FanotifyMark for path %s failed: %w", path, err)
	}

	return &FanotifyMonitor{
		fd:           fd,
		watchPath:    path,
		allowedPID:   pid,
		stopChan:     make(chan struct{}),
		stoppedChan:  make(chan struct{}),
		lockdownDeps: deps,
	}, nil
}

// Start запускает цикл мониторинга в отдельной горутине.
func (m *FanotifyMonitor) Start() {
	log.Printf("[Security] Starting fanotify monitor for path '%s', allowed PID: %d", m.watchPath, m.allowedPID)
	go m.runLoop()
}

// Stop останавливает мониторинг.
func (m *FanotifyMonitor) Stop() {
	log.Printf("[Security] Stopping fanotify monitor for path '%s'", m.watchPath)
	close(m.stopChan)
	// Отправляем пустой байт в пайп, чтобы разбудить read() если он заблокирован.
	// Это стандартный трюк для немедленной остановки.
	unix.Close(m.fd)
	<-m.stoppedChan
	log.Printf("[Security] Fanotify monitor stopped.", m.watchPath)
}

func (m *FanotifyMonitor) runLoop() {
	defer close(m.stoppedChan)

	// Буфер для чтения событий из ядра.
	buf := make([]byte, 4096)

	for {
		select {
		case <-m.stopChan:
			return
		default:
			// Блокируемся в ожидании события от ядра.
			n, err := unix.Read(m.fd, buf)
			if err != nil {
				// Если чтение прервано из-за закрытия fd, это штатное завершение.
				select {
				case <-m.stopChan:
					return
				default:
					log.Printf("ERROR: Fanotify read failed: %v", err)
				}
				return
			}

			if n == 0 {
				continue
			}

			// Обрабатываем все события, которые могли прийти в одном чтении.
			offset := 0
			for offset < n {
				metadata := (*unix.FanotifyEventMetadata)(unsafe.Pointer(&buf[offset]))

				if metadata.Mask&unix.FAN_OPEN_PERM == unix.FAN_OPEN_PERM {
					m.handlePermissionEvent(metadata)
				}

				// Переходим к следующему событию в буфере.
				offset += int(metadata.Event_len)
			}
		}
	}
}

// handlePermissionEvent принимает решение: разрешить или запретить доступ.
func (m *FanotifyMonitor) handlePermissionEvent(metadata *unix.FanotifyEventMetadata) {
	// Создаем структуру ответа.
	response := unix.FanotifyResponse{
		Fd:       metadata.Fd,
		Response: unix.FAN_DENY, // По умолчанию - запрещаем.
	}

	// Если PID процесса совпадает с разрешенным, меняем решение.
	if metadata.Pid == int32(m.allowedPID) {
		response.Response = unix.FAN_ALLOW
	} else {
		reason := fmt.Sprintf("Denied access to %s for unauthorized PID %d", m.watchPath, metadata.Pid)
		log.Printf("!!! SECURITY ALERT [fanotify] !!! %s", reason)
		go InitiateLockdown(m.lockdownDeps, reason)
	}

	// Отправляем наше решение обратно в ядро.
	responseBytes := (*[unsafe.Sizeof(response)]byte)(unsafe.Pointer(&response))[:]
	_, err := unix.Write(m.fd, responseBytes)
	if err != nil {
		log.Printf("ERROR: Failed to write fanotify response: %v", err)
	}

	// Ядро требует, чтобы мы закрыли файловый дескриптор, который оно нам передало.
	unix.Close(int(metadata.Fd))
}
