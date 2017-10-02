import { LogHackweekPage } from './app.po';

describe('log-hackweek App', function() {
  let page: LogHackweekPage;

  beforeEach(() => {
    page = new LogHackweekPage();
  });

  it('should display message saying app works', () => {
    page.navigateTo();
    expect(page.getParagraphText()).toEqual('app works!');
  });
});
