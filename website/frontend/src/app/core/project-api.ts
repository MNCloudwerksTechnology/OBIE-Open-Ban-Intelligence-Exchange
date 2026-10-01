import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';

/** The project stats endpoint (website/README.md, "Project API"). */
export const PROJECT_API = '/api/project';

/** A published release of OBIE. */
export interface Release {
  readonly tag: string;
  /** ISO-8601 timestamp. */
  readonly publishedAt: string;
  readonly url: string;
}

/** Live stats of the repository, as the back end last fetched them from GitHub. */
export interface ProjectStats {
  readonly repositoryUrl: string;
  readonly stars: number;
  readonly forks: number;
  readonly openIssues: number;
  readonly latestRelease: Release | null;
  /** ISO-8601 timestamp of the newest commit on the default branch. */
  readonly lastCommitAt: string;
}

/**
 * Client of the project stats endpoint. The browser only ever talks to the
 * site's own back end; GitHub is contacted server-side.
 */
@Injectable({ providedIn: 'root' })
export class ProjectApi {
  private readonly http = inject(HttpClient);

  /** The stats, or `undefined` when they are unavailable or cannot be loaded. */
  async stats(): Promise<ProjectStats | undefined> {
    try {
      const body = await firstValueFrom(this.http.get<unknown>(PROJECT_API));
      return isAvailable(body) ? body : undefined;
    } catch {
      return undefined;
    }
  }
}

/** Whether the body is an available `ProjectStats`; anything else is treated as unavailable. */
function isAvailable(body: unknown): body is ProjectStats {
  const stats = body as (Partial<ProjectStats> & { available?: unknown }) | null;
  return (
    stats?.available === true &&
    typeof stats.repositoryUrl === 'string' &&
    typeof stats.stars === 'number' &&
    typeof stats.lastCommitAt === 'string' &&
    (stats.latestRelease === null || typeof stats.latestRelease?.tag === 'string')
  );
}
