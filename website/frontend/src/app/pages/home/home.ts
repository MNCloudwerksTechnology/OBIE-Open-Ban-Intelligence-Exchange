import { ChangeDetectionStrategy, Component } from '@angular/core';

/** Placeholder home page; the landing page content follows in WP #1673. */
@Component({
  selector: 'app-home',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './home.html',
  styleUrl: './home.scss',
})
export class Home {}
