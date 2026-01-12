import { useEffect, useState, useCallback } from 'react';
import type { ReactElement } from 'react';
import { Link } from 'react-router-dom';
import { FaArrowLeft, FaSync, FaClock, FaCheckCircle, FaTimesCircle, FaBan, FaQuestionCircle, FaArrowUp, FaArrowDown, FaTimes, FaExclamationTriangle } from 'react-icons/fa';
import { cancelTask, updateTaskPriority, subscribeToQueueEvents } from '../api/client';
import type { Task, TaskEvent } from '../api/client';
import './QueuePage.css';

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

function formatDate(dateStr: string): string {
  return new Date(dateStr).toLocaleString();
}

function getStatusIcon(status: Task['status']): ReactElement {
  switch (status) {
    case 'pending': return <FaClock />;
    case 'running': return <FaSync />;
    case 'completed': return <FaCheckCircle />;
    case 'failed': return <FaTimesCircle />;
    case 'cancelled': return <FaBan />;
    default: return <FaQuestionCircle />;
  }
}

function getTaskTypeLabel(type: Task['type']): string {
  switch (type) {
    case 'repository_sync': return 'Repository';
    case 'lfs_sync': return 'LFS Object';
    default: return type;
  }
}

// Helper function to sort tasks: running first, then pending, then others, by priority then created_at
function sortTasks(tasks: Task[]): Task[] {
  return [...tasks].sort((a, b) => {
    const statusOrder = { running: 0, pending: 1, completed: 2, failed: 2, cancelled: 2 };
    const aOrder = statusOrder[a.status] ?? 2;
    const bOrder = statusOrder[b.status] ?? 2;
    if (aOrder !== bOrder) return aOrder - bOrder;
    if (a.priority !== b.priority) return b.priority - a.priority;
    return new Date(a.created_at).getTime() - new Date(b.created_at).getTime();
  });
}

export function QueuePage() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState<string>('all');
  const [connected, setConnected] = useState(false);

  const handleEvent = useCallback((event: TaskEvent) => {
    setLoading(false);
    setError(null);
    setConnected(true);
    
    if (event.type === 'init' && event.tasks) {
      setTasks(sortTasks(event.tasks));
    } else if (event.type === 'created' && event.task) {
      setTasks(prev => sortTasks([...prev, event.task!]));
    } else if (event.type === 'updated' && event.task) {
      setTasks(prev => {
        const updated = prev.map(t => t.id === event.task!.id ? event.task! : t);
        return sortTasks(updated);
      });
    } else if (event.type === 'deleted' && event.task) {
      setTasks(prev => prev.filter(t => t.id !== event.task!.id));
    }
  }, []);

  useEffect(() => {
    const cleanup = subscribeToQueueEvents(
      handleEvent,
      () => {
        setError('Connection lost. Reconnecting...');
        setConnected(false);
      }
    );

    return cleanup;
  }, [handleEvent]);

  const handleCancel = async (id: number) => {
    if (!confirm('Are you sure you want to cancel this task?')) return;
    try {
      await cancelTask(id);
      // SSE will update the task state
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to cancel task');
    }
  };

  const handlePriorityChange = async (id: number, delta: number) => {
    const task = tasks.find(t => t.id === id);
    if (!task) return;
    try {
      await updateTaskPriority(id, task.priority + delta);
      // SSE will update the task state
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to update priority');
    }
  };

  // Apply client-side filter
  const filteredTasks = filter === 'all' 
    ? tasks 
    : tasks.filter(t => t.status === filter);

  const activeCount = tasks.filter(t => t.status === 'running' || t.status === 'pending').length;

  return (
    <div className="queue-page">
      <header className="header">
        <h1>
          <Link to="/" className="back-link"><FaArrowLeft /></Link>
        </h1>
      </header>

      <main className="main-content">
        <div className="queue-header">
          <div className="queue-stats">
            <span className="stat">
              <strong>{activeCount}</strong> active tasks
            </span>
            <span className="stat">
              <strong>{tasks.length}</strong> total
            </span>
          </div>
          <div className="queue-filters">
            <select value={filter} onChange={(e) => setFilter(e.target.value)}>
              <option value="all">All Tasks</option>
              <option value="pending">Pending</option>
              <option value="running">Running</option>
              <option value="completed">Completed</option>
              <option value="failed">Failed</option>
              <option value="cancelled">Cancelled</option>
            </select>
            <span className={`connection-status ${connected ? 'connected' : 'disconnected'}`}>
              {connected ? '● Live' : '○ Connecting...'}
            </span>
          </div>
        </div>

        {loading && tasks.length === 0 ? (
          <div className="loading">Loading tasks...</div>
        ) : error ? (
          <div className="error">Error: {error}</div>
        ) : filteredTasks.length === 0 ? (
          <div className="no-tasks">
            <p>No tasks in the queue.</p>
          </div>
        ) : (
          <div className="task-list">
            {filteredTasks.map((task) => (
              <div key={task.id} className={`task-card status-${task.status}`}>
                <div className="task-header">
                  <div className="task-info">
                    <span className="task-status-icon">{getStatusIcon(task.status)}</span>
                    <span className="task-type">{getTaskTypeLabel(task.type)}</span>
                    <Link to={`/${task.repository}`} className="task-repo">
                      {task.repository}
                    </Link>
                  </div>
                  <div className="task-actions">
                    {task.status === 'pending' && (
                      <>
                        <button
                          className="btn btn-icon"
                          onClick={() => handlePriorityChange(task.id, 10)}
                          title="Increase priority"
                        >
                          <FaArrowUp />
                        </button>
                        <button
                          className="btn btn-icon"
                          onClick={() => handlePriorityChange(task.id, -10)}
                          title="Decrease priority"
                        >
                          <FaArrowDown />
                        </button>
                      </>
                    )}
                    {(task.status === 'pending' || task.status === 'running') && (
                      <button
                        className="btn btn-danger btn-icon"
                        onClick={() => handleCancel(task.id)}
                        title="Cancel task"
                      >
                        <FaTimes />
                      </button>
                    )}
                  </div>
                </div>

                {(task.status === 'running' || task.status === 'pending') && (
                  <div className="task-progress">
                    <div className="progress-bar">
                      <div
                        className="progress-fill"
                        style={{ width: `${task.progress}%` }}
                      />
                    </div>
                    <span className="progress-text">
                      {task.progress}%
                      {task.progress_msg && ` - ${task.progress_msg}`}
                    </span>
                    {task.total_bytes && task.total_bytes > 0 && (
                      <span className="progress-bytes">
                        {formatBytes(task.done_bytes || 0)} / {formatBytes(task.total_bytes)}
                      </span>
                    )}
                  </div>
                )}

                <div className="task-meta">
                  <span className="task-id">#{task.id}</span>
                  <span className="task-priority" title="Priority">
                    P{task.priority}
                  </span>
                  <span className="task-created">
                    Created: {formatDate(task.created_at)}
                  </span>
                  {task.started_at && (
                    <span className="task-started">
                      Started: {formatDate(task.started_at)}
                    </span>
                  )}
                  {task.completed_at && (
                    <span className="task-completed">
                      Completed: {formatDate(task.completed_at)}
                    </span>
                  )}
                </div>

                {task.error && (
                  <div className="task-error">
                    <FaExclamationTriangle /> {task.error}
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </main>
    </div>
  );
}
