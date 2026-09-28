import { ProjectStats } from '../app/core/project-api';

/** Stats as the back end returns them when GitHub answered. */
export const STATS: ProjectStats = {
  repositoryUrl: 'https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange',
  stars: 1234,
  forks: 7,
  openIssues: 3,
  latestRelease: {
    tag: 'v0.1.0',
    publishedAt: '2026-09-01T12:00:00Z',
    url: 'https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange/releases/tag/v0.1.0',
  },
  lastCommitAt: '2026-09-20T10:30:00Z',
};
