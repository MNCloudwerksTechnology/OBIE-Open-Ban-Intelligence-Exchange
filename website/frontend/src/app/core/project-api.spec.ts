import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';

import { STATS } from '../../testing/project-stats';
import { PROJECT_API, ProjectApi } from './project-api';

describe('ProjectApi', () => {
  let api: ProjectApi;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    api = TestBed.inject(ProjectApi);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('returns the stats when they are available', async () => {
    const stats = api.stats();
    const request = http.expectOne(PROJECT_API);
    expect(request.request.method).toBe('GET');
    request.flush({ available: true, ...STATS });
    expect(await stats).toEqual({ available: true, ...STATS });
  });

  it('accepts stats without a release', async () => {
    const stats = api.stats();
    http.expectOne(PROJECT_API).flush({ available: true, ...STATS, latestRelease: null });
    expect((await stats)?.latestRelease).toBeNull();
  });

  it('returns nothing when the back end reports them unavailable', async () => {
    const stats = api.stats();
    http.expectOne(PROJECT_API).flush({
      available: false,
      repositoryUrl: null,
      stars: null,
      forks: null,
      openIssues: null,
      latestRelease: null,
      lastCommitAt: null,
    });
    expect(await stats).toBeUndefined();
  });

  it('returns nothing for a body of another shape', async () => {
    const stats = api.stats();
    http.expectOne(PROJECT_API).flush({ available: true, stars: '1234' });
    expect(await stats).toBeUndefined();
  });

  it('returns nothing on an error', async () => {
    const stats = api.stats();
    http.expectOne(PROJECT_API).flush(null, { status: 502, statusText: 'Bad Gateway' });
    expect(await stats).toBeUndefined();
  });

  it('returns nothing on a network error', async () => {
    const stats = api.stats();
    http.expectOne(PROJECT_API).error(new ProgressEvent('error'));
    expect(await stats).toBeUndefined();
  });
});
