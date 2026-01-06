// API client for interacting with the gitd backend

export interface TreeEntry {
  name: string;
  path: string;
  type: 'blob' | 'tree';
  mode: string;
  sha: string;
  isLfs?: boolean;
  blobSha256?: string;
}

export interface BlobContent {
  content: string;
  size: number;
  contentType: string;
}

export interface Commit {
  sha: string;
  message: string;
  author: string;
  email: string;
  date: string;
}

export interface Branch {
  name: string;
  current: boolean;
}

export interface RepoInfo {
  name: string;
  default_branch: string;
  description: string;
}

export interface RepoListItem {
  name: string;
  is_mirror?: boolean;
}

const API_BASE = '/api';

export async function fetchTree(repo: string, ref: string, path: string = ''): Promise<TreeEntry[]> {
  const url = path 
    ? `${API_BASE}/${repo}/tree/${ref}/${path}`
    : `${API_BASE}/${repo}/tree/${ref}`;
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error('Failed to fetch tree');
  }
  return response.json();
}

export async function fetchBlob(repo: string, ref: string, path: string): Promise<BlobContent> {
  const response = await fetch(`${API_BASE}/${repo}/blob/${ref}/${path}`);
  if (!response.ok) {
    throw new Error('Failed to fetch blob');
  }
  const content = await response.text();
  const contentType = response.headers.get('Content-Type') || 'text/plain';
  const size = parseInt(response.headers.get('Content-Length') || '0', 10);
  return { content, size, contentType };
}

export async function fetchCommits(repo: string, ref: string): Promise<Commit[]> {
  const response = await fetch(`${API_BASE}/${repo}/commits/${ref}`);
  if (!response.ok) {
    throw new Error('Failed to fetch commits');
  }
  return response.json();
}

export async function fetchBranches(repo: string): Promise<Branch[]> {
  const response = await fetch(`${API_BASE}/${repo}/branches`);
  if (!response.ok) {
    throw new Error('Failed to fetch branches');
  }
  return response.json();
}

export async function fetchRepoInfo(repo: string): Promise<RepoInfo> {
  const response = await fetch(`${API_BASE}/${repo}`);
  if (!response.ok) {
    throw new Error('Failed to fetch repo info');
  }
  return response.json();
}

export async function fetchRepos(): Promise<RepoListItem[]> {
  const response = await fetch(`${API_BASE}/repos`);
  if (!response.ok) {
    throw new Error('Failed to fetch repos');
  }
  return response.json();
}

export interface ImportStatus {
  status: string;
  step: string;
  progress: number;
  total_steps: number;
  error?: string;
}

export async function createRepository(name: string): Promise<void> {
  const response = await fetch(`${API_BASE}/repositories/${name}`, {
    method: 'POST',
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || 'Failed to create repository');
  }
}

export async function deleteRepository(name: string): Promise<void> {
  const response = await fetch(`${API_BASE}/repositories/${name}`, {
    method: 'DELETE',
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || 'Failed to delete repository');
  }
}

export async function importRepository(name: string, sourceUrl: string): Promise<void> {
  const response = await fetch(`${API_BASE}/repositories/${name}/import`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ source_url: sourceUrl }),
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || 'Failed to start import');
  }
}

export async function getImportStatus(name: string): Promise<ImportStatus> {
  const response = await fetch(`${API_BASE}/repositories/${name}/import/status`);
  if (!response.ok) {
    throw new Error('Failed to get import status');
  }
  return response.json();
}

export async function syncRepository(name: string): Promise<void> {
  const response = await fetch(`${API_BASE}/repositories/${name}/sync`, {
    method: 'POST',
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || 'Failed to start sync');
  }
}
