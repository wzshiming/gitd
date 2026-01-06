import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { fetchRepos } from '../api/client';
import type { RepoListItem } from '../api/client';
import './HomePage.css';

export function HomePage() {
  const [repos, setRepos] = useState<RepoListItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    fetchRepos()
      .then(setRepos)
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false));
  }, []);

  if (loading) {
    return <div className="loading">Loading repositories...</div>;
  }

  if (error) {
    return <div className="error">Error: {error}</div>;
  }

  return (
    <div className="home-page">
      <header className="header">
        <h1>
          <span className="logo">🗂️</span>
          gitd
        </h1>
      </header>
      <main className="main-content">
        <h2>Repositories</h2>
        {repos.length === 0 ? (
          <p className="no-repos">No repositories found.</p>
        ) : (
          <ul className="repo-list">
            {repos.map((repo) => (
              <li key={repo.name} className="repo-item">
                <Link to={`/${repo.name}`} className="repo-link">
                  <span className="repo-icon">📁</span>
                  <span className="repo-full-name">
                    <strong>{repo.name}</strong>
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </main>
    </div>
  );
}
