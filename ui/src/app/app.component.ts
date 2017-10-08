import { Component } from '@angular/core';

// Services
import { ChartsService } from  './charts.service';

@Component({
  selector: 'app-root',
  templateUrl: './app.component.html',
  styleUrls: ['./app.component.scss'],
})

export class AppComponent {

    googleLoaded: boolean = false;

    constructor(private chartsService: ChartsService) {
        let script = document.createElement('script');
        script.src = '//www.google.com/jsapi';
        script.onload = () => {
            (<any>window).google.charts.load("visualization", "1", {packages: ["corechart", "gauge"]});
            this.googleLoaded = true;
            this.chartsService.createWebSocketConnection();
        };
        document.head.appendChild(script);
    }
}
